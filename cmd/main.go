// KubeWA Operator – the first Kubernetes WhatsApp Operator.
// It converts WhatsApp (via Twilio) into a bidirectional, native interface for
// Kubernetes incident management.
package main

import (
	"flag"
	"net/http"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kubewav1alpha1 "github.com/takumi-software/kubewa-operator/api/v1alpha1"
	"github.com/takumi-software/kubewa-operator/internal/actions"
	aiclient "github.com/takumi-software/kubewa-operator/internal/ai"
	"github.com/takumi-software/kubewa-operator/internal/controller"
	twiliointernal "github.com/takumi-software/kubewa-operator/internal/twilio"
	"github.com/takumi-software/kubewa-operator/internal/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kubewav1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		webhookAddr          string
		webhookPublicURL     string

		twilioAccountSID string
		twilioAuthToken  string
		twilioFromNumber string

		ollamaURL   string
		ollamaModel string

		allowedPhones string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&webhookAddr, "webhook-bind-address", ":8082", "The address the Twilio webhook HTTP server binds to.")
	flag.StringVar(&webhookPublicURL, "webhook-public-url", "", "The public URL Twilio will POST to (for signature verification).")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&twilioAccountSID, "twilio-account-sid", os.Getenv("TWILIO_ACCOUNT_SID"), "Twilio Account SID.")
	flag.StringVar(&twilioAuthToken, "twilio-auth-token", os.Getenv("TWILIO_AUTH_TOKEN"), "Twilio Auth Token.")
	flag.StringVar(&twilioFromNumber, "twilio-from-number", os.Getenv("TWILIO_FROM_NUMBER"), "Twilio WhatsApp from number (e.g. whatsapp:+14155238886).")
	flag.StringVar(&ollamaURL, "ollama-url", envOrDefault("OLLAMA_URL", "http://localhost:11434"), "Ollama API base URL.")
	flag.StringVar(&ollamaModel, "ollama-model", envOrDefault("OLLAMA_MODEL", "llama3"), "Ollama model name.")
	flag.StringVar(&allowedPhones, "allowed-phones", os.Getenv("ALLOWED_PHONES"), "Comma-separated whitelist of E.164 phone numbers allowed to interact via WhatsApp.")

	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Validate required Twilio config.
	if twilioAccountSID == "" || twilioAuthToken == "" || twilioFromNumber == "" {
		setupLog.Error(nil, "Twilio credentials are required",
			"hint", "set TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, TWILIO_FROM_NUMBER env vars or flags")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "kubewa-operator.kubewa.dev",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Build typed and dynamic Kubernetes clients.
	restConfig := mgr.GetConfig()
	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes client")
		os.Exit(1)
	}
	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		setupLog.Error(err, "unable to create dynamic client")
		os.Exit(1)
	}

	// Build Twilio client.
	twilioClient := twiliointernal.NewClient(twilioAccountSID, twilioAuthToken, twilioFromNumber)

	// Build AI client (optional – errors are non-fatal).
	aiClt := aiclient.NewClient(ollamaURL, ollamaModel)

	// Build actions executor.
	exec := actions.NewExecutor(k8sClient, dynClient)

	// Register OnCallSchedule controller.
	if err := (&controller.OnCallScheduleReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controllers").WithName("OnCallSchedule"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "OnCallSchedule")
		os.Exit(1)
	}

	// Register Incident controller.
	if err := (&controller.IncidentReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Log:          ctrl.Log.WithName("controllers").WithName("Incident"),
		TwilioClient: twilioClient,
		AIClient:     aiClt,
		Recorder:     mgr.GetEventRecorderFor("kubewa-operator"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Incident")
		os.Exit(1)
	}

	// Health probes.
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Start the Twilio webhook HTTP server in a goroutine.
	phones := parseAllowedPhones(allowedPhones)
	wh := webhook.NewHandler(
		mgr.GetClient(),
		twilioClient,
		aiClt,
		exec,
		ctrl.Log.WithName("webhook"),
		webhookPublicURL,
		phones,
	)
	go func() {
		setupLog.Info("starting Twilio webhook server", "addr", webhookAddr)
		srv := &http.Server{
			Addr:         webhookAddr,
			Handler:      http.HandlerFunc(wh.ServeHTTP),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			setupLog.Error(err, "webhook server error")
			os.Exit(1)
		}
	}()

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func parseAllowedPhones(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
