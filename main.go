package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/support"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Trusted Advisor is only available on us-east-1
const SupportRegion = "us-east-1"

// Trusted Advisor supports two languages, "en" and "ja"... And unfortunately I can't speak Japanese
const Lang = "en"

// Trusted Advisor supports the following statuses
var Statuses = []string{"ok", "warning", "error", "not_available"}

type checkJob struct {
	checkId       string
	checkName     string
	checkCategory string
}

func refreshChecks(ctx context.Context, svc *support.Client, taGaugeVec *prometheus.GaugeVec, concurrency int) {
	log.Printf("refreshing trusted advisor checks and statuses")

	params := &support.DescribeTrustedAdvisorChecksInput{
		Language: aws.String(Lang),
	}
	resp, err := svc.DescribeTrustedAdvisorChecks(ctx, params)

	if err != nil {
		log.Fatalf("cannot describe trusted advisor checks: %v", err)
	}

	jobs := make(chan checkJob, len(resp.Checks))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				log.Printf("worker %d refreshing check %s (id %s, category %s)", i, job.checkName, job.checkId, job.checkCategory)
				refreshSpecificCheck(ctx, svc, job.checkId, job.checkName, job.checkCategory, taGaugeVec)
			}
		}()
	}

	// Send jobs
	log.Printf("refreshing %d checks with %d workers", len(resp.Checks), concurrency)
	for _, check := range resp.Checks {
		jobs <- checkJob{
			checkId:       *check.Id,
			checkName:     *check.Name,
			checkCategory: *check.Category,
		}
	}
	close(jobs)

	// Wait for all workers to complete
	wg.Wait()
}

func refreshSpecificCheck(ctx context.Context, svc *support.Client, checkId string, checkName string, checkCategory string, taGaugeVec *prometheus.GaugeVec) {
	params := &support.DescribeTrustedAdvisorCheckResultInput{
		CheckId:  aws.String(checkId),
		Language: aws.String(Lang),
	}

	var resp *support.DescribeTrustedAdvisorCheckResultOutput
	var err error
	for retries := 0; retries < 3; retries++ {
		resp, err = svc.DescribeTrustedAdvisorCheckResult(ctx, params)
		if err == nil {
			break
		}
		log.Printf("error refreshing check %s (id %s): %v (attempt %d/3)", checkName, checkId, err, retries+1)
		time.Sleep(time.Second * time.Duration(retries+1))
	}

	if err != nil {
		log.Printf("cannot describe trusted advisor check result: %v", err)
		return
	}

	// Clean up potential outdated gauge values
	for _, s := range Statuses {
		taGaugeVec.DeleteLabelValues(
			checkId,
			checkName,
			checkCategory,
			s,
		)
	}

	// And set the current value
	result := *resp.Result
	taGaugeVec.WithLabelValues(
		checkId,
		checkName,
		checkCategory,
		*result.Status,
	).Add(float64(len(result.FlaggedResources)))
}

func refreshChecksPeriodically(ctx context.Context, svc *support.Client, taGaugeVec *prometheus.GaugeVec, period int, concurrency int) {
	ticker := time.NewTicker(time.Duration(period) * time.Second)
	for range ticker.C {
		refreshChecks(ctx, svc, taGaugeVec, concurrency)
	}
}

func getEnv(key, fallback string) string {
	value, isSet := os.LookupEnv(key)
	if isSet {
		return value
	} else {
		return fallback
	}
}

func main() {
	ctx := context.Background()

	// Read configuration from env
	listenAddr := getEnv("LISTEN_ADDR", ":2112")
	refreshPeriodStr := getEnv("REFRESH_PERIOD", "300")
	refreshPeriod, err := strconv.Atoi(refreshPeriodStr)
	if err != nil {
		log.Fatalf("cannot convert REFRESH_PERIOD to int: %v", err)
	}
	concurrencyStr := getEnv("CONCURRENCY", "10")
	concurrency, err := strconv.Atoi(concurrencyStr)
	if err != nil {
		log.Fatalf("cannot convert CONCURRENCY to int: %v", err)
	}

	log.Printf("trusted advisor exporter starting up")
	log.Printf("listening on %s", listenAddr)
	log.Printf("refresh period %d seconds", refreshPeriod)
	log.Printf("concurrency %d", concurrency)

	// Set up AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(SupportRegion))
	if err != nil {
		log.Fatalf("unable to load SDK config: %v", err)
	}
	svc := support.NewFromConfig(cfg)

	// Prometheus config: set up a clean registry...
	registry := prometheus.NewPedanticRegistry()
	gatherer := prometheus.Gatherer(registry)

	// ... and create a vector of Trusted Advisor gauges
	taGaugeVec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "aws_trusted_advisor_check",
			Help: "AWS Trusted Advisor check result",
		},
		[]string{
			"checkid",
			"name",
			"category",
			"status",
		},
	)
	registry.MustRegister(taGaugeVec)

	// Populate our checks once at start-up time
	refreshChecks(ctx, svc, taGaugeVec, concurrency)

	// And set up a periodic refresh
	go refreshChecksPeriodically(ctx, svc, taGaugeVec, int(refreshPeriod), concurrency)

	// Finally, serve metrics on /metrics
	http.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))
	http.ListenAndServe(listenAddr, nil)
}
