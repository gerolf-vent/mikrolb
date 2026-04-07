package core

import (
	"context"
	"net/url"
	"time"

	"github.com/sethvargo/go-envconfig"
)

type Config struct {
	// Metrics and health probe options
	MetricsAddr string `env:"METRICS_ADDR,default=:8080"`
	ProbeAddr   string `env:"PROBE_ADDR,default=:8081"`

	// Webhook options
	WebhookPort    int    `env:"WEBHOOK_PORT,default=9443"`
	WebhookCertDir string `env:"WEBHOOK_CERT_DIR,default=/mnt/k8s-webhook-server/serving-certs"`

	// RouterOS options
	RouterOSURL      *url.URL `env:"ROUTEROS_URL,required"`
	RouterOSUsername string   `env:"ROUTEROS_USERNAME,required"`
	RouterOSPassword string   `env:"ROUTEROS_PASSWORD,required"`
	RouterOSCACert   string   `env:"ROUTEROS_CA_CERT"`

	// Controller options
	LoadBalancerClassName string `env:"LOAD_BALANCER_CLASS_NAME,default=mikrolb.de/controller"`
	IsDefaultLoadBalancer bool   `env:"LOAD_BALANCER_DEFAULT,default=false"`

	// Reconciliation options
	AllocationTimeout time.Duration `env:"ALLOCATION_TIMEOUT,default=5m"`
}

func LoadConfig(ctx context.Context) (*Config, error) {
	var cfg Config
	if err := envconfig.Process(ctx, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
