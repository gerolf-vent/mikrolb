package controller

import (
	"fmt"
	"go/build"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	v1alpha1 "github.com/gerolf-vent/mikrolb/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	cfg        *rest.Config
	testScheme *runtime.Scheme
)

func TestMain(m *testing.M) {
	os.Setenv("PATH", fmt.Sprintf("%s/bin:%s", build.Default.GOPATH, os.Getenv("PATH")))

	// Call setup-envtest
	out, err := exec.Command("setup-envtest", "use", "-p", "path").Output()
	if err != nil {
		log.Fatalf("failed to setup test environment: %v", err)
	}
	os.Setenv("KUBEBUILDER_ASSETS", strings.TrimSpace(string(out)))

	// Build client scheme
	testScheme = runtime.NewScheme()
	if err := scheme.AddToScheme(testScheme); err != nil {
		log.Fatalf("failed to add k8s scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(testScheme); err != nil {
		log.Fatalf("failed to add mikrolb scheme: %v", err)
	}

	// Create test environment with MetalLB CRDs
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Dir("../../config/crd/bases/")},
		Scheme:            testScheme,
	}
	// Enable dual-stack networking on the API server so tests can create
	// services with both IPv4 and IPv6 families.
	testEnv.ControlPlane.GetAPIServer().Configure().
		Append("service-cluster-ip-range", "10.0.0.0/24,fd00::/120")

	// Start test environment
	cfg, err = testEnv.Start()
	if err != nil {
		log.Fatalf("failed to start test environment: %v", err)
	}

	// Run all tests, capture exit code
	code := m.Run()

	os.Exit(code) // must call this, or the process won't exit with the right code
}

func getTestRecorder() events.EventRecorder {
	return &fakeEventRecorder{}
}

type fakeEventRecorder struct{}

func (f *fakeEventRecorder) Eventf(regarding runtime.Object, related runtime.Object, eventtype, reason, action, note string, args ...interface{}) {
}
