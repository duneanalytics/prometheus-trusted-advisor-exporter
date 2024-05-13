package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/support"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Trusted Advisor is only available on us-east-1
const SupportRegion = "us-east-1"

// Trusted Advisor supports two languages, "en" and "ja"... And unfortunately I can't speak Japanese
const Lang = "en"

// Trusted Advisor supports the following statuses
var Statuses = []string{"ok", "warning", "error", "not_available"}

func refreshChecks(svc *support.Support, taGaugeVec *prometheus.GaugeVec) {
	log.Printf("refreshing trusted advisor checks and statuses")

	// Get all checks...
	params := support.DescribeTrustedAdvisorChecksInput{Language: aws.String(Lang)}
	resp, err := svc.DescribeTrustedAdvisorChecks(&params)

	if err != nil {
		log.Fatalf("cannot describe trusted advisor checks: %w", err)
	}

	log.Printf("refreshing %d checks", len(resp.Checks))
	for _, check := range resp.Checks {
		go refreshSpecificCheck(svc, *check.Id, *check.Name, *check.Category, taGaugeVec)
	}
}

func refreshSpecificCheck(svc *support.Support, checkId string, checkName string, checkCategory string, taGaugeVec *prometheus.GaugeVec) {
	params := support.DescribeTrustedAdvisorCheckResultInput{
		CheckId:  aws.String(checkId),
		Language: aws.String(Lang),
	}
	resp, err := svc.DescribeTrustedAdvisorCheckResult(&params)

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
	// log.Printf("%s", result)
	if result.ResourcesSummary != nil && result.ResourcesSummary.ResourcesFlagged != nil {
		taGaugeVec.WithLabelValues(
			checkId,
			checkName,
			checkCategory,
			*result.Status,
		).Add(float64(*result.ResourcesSummary.ResourcesFlagged))
	} else {
		taGaugeVec.WithLabelValues(
			checkId,
			checkName,
			checkCategory,
			*result.Status,
		).Add(float64(len(result.FlaggedResources)))
	}
}

func refreshChecksPeriodically(svc *support.Support, taGaugeVec *prometheus.GaugeVec, period int) {
	ticker := time.NewTicker(time.Duration(period) * time.Second)
	for _ = range ticker.C {
		refreshChecks(svc, taGaugeVec)
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
	// Read configuration from env
	listenAddr := getEnv("LISTEN_ADDR", ":2112")
	refreshPeriodStr := getEnv("REFRESH_PERIOD", "300")
	refreshPeriod, err := strconv.Atoi(refreshPeriodStr)
	if err != nil {
		log.Fatalf("cannot convert REFRESH_PERIOD to int: %w", err)
	}

	log.Printf("trusted advisor exporter starting up")
	log.Printf("listening on %s", listenAddr)
	log.Printf("refresh period %d seconds", refreshPeriod)

	// Set up AWS session based on shared config
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	svc := support.New(sess, aws.NewConfig().WithRegion(SupportRegion))

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
	refreshChecks(svc, taGaugeVec)

	// And set up a periodic refresh
	go refreshChecksPeriodically(svc, taGaugeVec, int(refreshPeriod))

	// Finally, serve metrics on /metrics
	http.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))
	http.ListenAndServe(listenAddr, nil)
}
