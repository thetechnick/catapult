/*
Copyright 2020 The Catapult authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package agent

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/thetechnick/catapult/internal/agent/controllers"
)

type flags struct {
	metricsAddr, healthAddr string
	enableLeaderElection    bool
	certDir                 string
	developmentLogger       bool

	kind, version, group string
	remoteNamespaces     string
}

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

func Run() {
	if err := run(); err != nil {
		setupLog.Error(err, "crashed")
		os.Exit(1)
	}
}

func run() error {
	// Flags
	flags := &flags{}
	flag.StringVar(&flags.metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&flags.healthAddr, "health-addr", ":9440", "The address the health endpoint binds to.")
	flag.StringVar(&flags.certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs", "The webhook TLS certificates directory.")
	flag.BoolVar(&flags.enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for operator. Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&flags.developmentLogger, "developmentLogger", false, "enables the development logger instead of the production logger (more verbosity, text instead of json).")

	flag.StringVar(&flags.kind, "kind", "", "Type Kind.")
	flag.StringVar(&flags.group, "group", "", "Type API Group.")
	flag.StringVar(&flags.version, "version", "", "Type API Version.")
	flag.StringVar(&flags.remoteNamespaces, "remote-namespace", "", "Namespaces in the remote cluster to watch and sync to.")

	flag.Parse()

	// Logger
	ctrl.SetLogger(zap.Logger(flags.developmentLogger))

	// Manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: flags.metricsAddr,
		Port:               9443,
		LeaderElection:     flags.enableLeaderElection,
		LeaderElectionID:   flags.kind + ".catapult.thetechnick.ninja",
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	if err = (&controllers.SyncReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("SyncReconciler"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create SyncReconciler controller: %w", err)
	}

	if err = (&controllers.AdoptionReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("AdoptionReconciler"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create AdoptionReconciler controller: %w", err)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("error running manager: %w", err)
	}
	return nil
}
