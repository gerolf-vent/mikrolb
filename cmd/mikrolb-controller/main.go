package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	v1alpha1 "github.com/gerolf-vent/mikrolb/api/v1alpha1"
	"github.com/gerolf-vent/mikrolb/internal/controller"
	"github.com/gerolf-vent/mikrolb/internal/core"
	"github.com/gerolf-vent/mikrolb/internal/routeros"
	"github.com/gerolf-vent/mikrolb/internal/routeros/api"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Initialize logger
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("Setup")

	// Load configuration from environment variables
	config, err := core.LoadConfig(context.Background())
	if err != nil {
		setupLog.Error(err, "failed to load configuration")
		os.Exit(1)
	}

	// Load TLS configuration for RouterOS API client
	var tlsConfig *tls.Config
	if config.RouterOSCACert != "" {
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM([]byte(config.RouterOSCACert)) {
			setupLog.Error(fmt.Errorf("failed to append CA certificate"), "invalid CA certificate", "content", config.RouterOSCACert)
			os.Exit(1)
		}

		tlsConfig = &tls.Config{
			RootCAs: caCertPool,
		}
	}

	// Initialize RouterOS API client
	setupLog.Info("connecting to RouterOS", "endpoint", config.RouterOSURL.String(), "username", config.RouterOSUsername)
	apiClient := api.NewClient(config.RouterOSURL, tlsConfig, ctrl.Log.WithName("RouterOS"))
	apiClient.SetCredentials(config.RouterOSUsername, config.RouterOSPassword)

	// Create service manager
	serviceManager := routeros.NewBackend(apiClient, ctrl.Log.WithName("ServiceManager"))

	// Verify RouterOS connection
	routerOSVersion, err := serviceManager.Check()
	if err != nil {
		if api.IsUnauthorized(err) || api.IsForbidden(err) {
			setupLog.Error(err, "authentication failed, check credentials", "endpoint", config.RouterOSURL.String(), "username", config.RouterOSUsername)
		} else {
			setupLog.Error(err, "failed to check RouterOS version", "endpoint", config.RouterOSURL.String(), "username", config.RouterOSUsername)
		}
		os.Exit(1)
	}
	setupLog.Info("connected to RouterOS", "endpoint", config.RouterOSURL.String(), "username", config.RouterOSUsername, "version", routerOSVersion)

	// Setup service manager
	setupLog.Info("setting up load balancer infrastructure on RouterOS")
	if err := serviceManager.Setup(); err != nil {
		setupLog.Error(err, "failed to setup service manager")
		os.Exit(1)
	}
	setupLog.Info("load balancer infrastructure setup complete")

	// Create controller manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: config.MetricsAddr,
		},
		HealthProbeBindAddress: config.ProbeAddr,
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    config.WebhookPort,
			CertDir: config.WebhookCertDir,
		}),
		LeaderElection:   true,
		LeaderElectionID: "mikrolb-controller",
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Attach IPPool validating webhook
	if err := controller.AttachIPPoolWebhook(mgr); err != nil {
		setupLog.Error(err, "unable to attach IPPool validating webhook")
		os.Exit(1)
	}

	// Attach IPPool controller
	if err := controller.AttachIPPoolController(mgr); err != nil {
		setupLog.Error(err, "unable to attach IPPool controller")
		os.Exit(1)
	}

	// Attach IPAllocation controller
	if err := controller.AttachIPAllocationController(mgr, serviceManager, config); err != nil {
		setupLog.Error(err, "unable to attach IPAllocation controller")
		os.Exit(1)
	}

	// Attach Service controller
	if err := controller.AttachServiceController(mgr, serviceManager, config); err != nil {
		setupLog.Error(err, "unable to attach Service controller")
		os.Exit(1)
	}

	// Add health and ready checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
