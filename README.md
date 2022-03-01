# prometheus-trusted-advisor-exporter

A Prometheus exporter for [AWS Trusted Advisor](https://aws.amazon.com/premiumsupport/technology/trusted-advisor/).

## Why?

Trusted Advisor [exposes metrics in Cloudwatch](https://docs.aws.amazon.com/awssupport/latest/user/cloudwatch-metrics-ta.html), so one could be tempted to use the [Cloudwatch exporter](https://github.com/prometheus/cloudwatch_exporter) to get Trusted Advisor metrics.

However, this approach suffers from various issues:
- Trusted Advisor only publishes metrics when it refreshes its checks, which creates large gaps in the metrics
- In turn, this makes the Cloudwatch exporter highly unreliable - with Cloudwatch being full of holes, the metrics exported to Prometheus will be inconsistent as well
- You can _partly_ work around that issue by configuring the Cloudwatch exporter to request old data with `range_seconds`, but scrapes then become extremely long (120+ seconds) and the whole setup will start being expensive, as Cloudwatch isn't cheap
- A minor issue in comparison - Trusted Advisor only publishes its metrics in us-east-1, so your Cloudwatch exporter needs to be configured for that region

Instead, this exporter retrieves data directly from the [Support API](https://docs.aws.amazon.com/sdk-for-go/api/service/support/) in order to always get up-to-date and correct data. This means you will need a [support plan of Business or above](https://aws.amazon.com/premiumsupport/plans/) to use this exporter.

Finally, unlike Cloudwatch, this API is also free. Well, "free" in the sense that it's included in the cost of your support contract. 🙂

## Credentials and permissions

`prometheus-trusted-advisor-exporter` uses the standard AWS authentication methods provided by the AWS SDK for Go, so you should be able to authenticate using the standard environment variables, shared credentials file, IAM roles for EC2, etc. See [Specifying Credentials](https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/configuring-sdk.html#specifying-credentials) for more details.

It requires the following permissions:
- `support:DescribeTrustedAdvisorChecks`
- `support:DescribeTrustedAdvisorCheckResult`

## Configuration

`prometheus-trusted-advisor-exporter` supports the following environment variables for configuration:

| Variable         | Description   | Default value |
|------------------| ------------- |---------------|
| `LISTEN_ADDR`    | Address and port the exporter will listen to  | `:2112`       |
| `REFRESH_PERIOD` | How often to refresh all checks and their values, in seconds  | `300`         |

## Build and run

```bash
go build
./prometheus-trusted-advisor-exporter
```

Or use the Docker container:

```bash
docker build . -t prometheus-trusted-advisor-exporter
docker run -p 2112:2112 -it prometheus-trusted-advisor-exporter
```

## Exposed metrics

`prometheus-trusted-advisor-exporter` exports a single gauge, `aws_trusted_advisor_check`, with classification labels:
```
# HELP aws_trusted_advisor_check AWS Trusted Advisor check result
# TYPE aws_trusted_advisor_check gauge
aws_trusted_advisor_check{category="cost_optimizing",checkid="1e93e4c0b5",name="Amazon EC2 Reserved Instance Lease Expiration",status="ok"} 0
aws_trusted_advisor_check{category="cost_optimizing",checkid="1qazXsw23e",name="Amazon Relational Database Service (RDS) Reserved Instance Optimization",status="warning"} 8
aws_trusted_advisor_check{category="cost_optimizing",checkid="1qw23er45t",name="Amazon Redshift Reserved Node Optimization",status="ok"} 0
aws_trusted_advisor_check{category="cost_optimizing",checkid="51fC20e7I2",name="Amazon Route 53 Latency Resource Record Sets",status="ok"} 0
aws_trusted_advisor_check{category="security",checkid="DqdJqYeRm5",name="IAM Access Key Rotation",status="error"} 36
(...)
```

All Trusted Advisor checks are exported for every scrape, regardless of their status. They will get a status of "ok" (green), "warning" (yellow), "error" (red), or "not_available" if the check failed to refresh.
