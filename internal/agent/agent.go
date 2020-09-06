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
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	catapultv1alpha1 "github.com/thetechnick/catapult/api/v1alpha1"
	"github.com/thetechnick/catapult/internal/agent/controllers"
)

type flags struct {
	metricsAddr, healthAddr string
	enableLeaderElection    bool
	certDir                 string
	developmentLogger       bool

	kind, version, group    string
	remoteNamespaces        string
	remoteClusterKubeconfig string
	remoteClusterName       string
}

var (
	scheme       = runtime.NewScheme()
	remoteScheme = runtime.NewScheme()
	setupLog     = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(remoteScheme))
	utilruntime.Must(catapultv1alpha1.AddToScheme(scheme))
}

func Run() {
	if err := run(); err != nil {
		setupLog.Error(err, "crashed")
		os.Exit(1)
	}
}

func run() error {
	ctrl.SetLogger(zap.Logger(true))

	// Flags
	flags := &flags{}
	flag.StringVar(&flags.metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&flags.healthAddr, "health-addr", ":9440", "The address the health endpoint binds to.")
	flag.StringVar(&flags.certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs", "The webhook TLS certificates directory.")
	flag.BoolVar(&flags.enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for operator. Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&flags.developmentLogger, "developmentLogger", true, "enables the development logger instead of the production logger (more verbosity, text instead of json).")

	flag.StringVar(&flags.remoteClusterName, "remote-cluster-name", "", "Remote cluster name.")
	flag.StringVar(&flags.kind, "kind", "", "Type Kind.")
	flag.StringVar(&flags.group, "group", "", "Type API Group.")
	flag.StringVar(&flags.version, "version", "", "Type API Version.")
	flag.StringVar(&flags.remoteNamespaces, "remote-namespaces", "", "Namespaces in the remote cluster to watch and sync to.")
	flag.StringVar(&flags.remoteClusterKubeconfig, "remote-cluster-kubeconfig", "/secret/remote-cluster-kubeconfig.yaml", "Remote cluster kubeconfig.")

	flag.Parse()

	// Validate
	if flags.remoteClusterName == "" {
		return fmt.Errorf("-remote-cluster-name must be set")
	}
	if flags.kind == "" {
		return fmt.Errorf("-kind must be set")
	}
	// if flags.group == "" {
	// 	return fmt.Errorf("-group must be set")
	// }
	if flags.version == "" {
		return fmt.Errorf("-version must be set")
	}
	if flags.remoteNamespaces == "" {
		return fmt.Errorf("-remoteNamespaces must be set")
	}
	remoteNamespaces := strings.Split(flags.remoteNamespaces, ",")

	// Logger
	ctrl.SetLogger(zap.Logger(flags.developmentLogger))

	// Manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     flags.metricsAddr,
		HealthProbeBindAddress: flags.healthAddr,
		Port:                   9443,
		LeaderElection:         flags.enableLeaderElection,
		LeaderElectionID:       flags.kind + ".catapult.thetechnick.ninja",
		CertDir:                flags.certDir,
		NewClient: func(cache cache.Cache, config *rest.Config, options client.Options) (client.Client, error) {
			// Create the Client for Write operations.
			c, err := client.New(config, options)
			if err != nil {
				return nil, err
			}

			// we don't want a client.DelegatingReader here,
			// because we WANT to cache unstructured objects.
			return &client.DelegatingClient{
				Reader:       cache,
				Writer:       c,
				StatusClient: c,
			}, nil
		},
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	// RemoteCluster Client
	remoteCfg, err := clientcmd.BuildConfigFromFlags("", flags.remoteClusterKubeconfig)
	if err != nil {
		return fmt.Errorf("reading remote cluster kubeconfig: %w", err)
	}
	remoteMapper, err := apiutil.NewDiscoveryRESTMapper(remoteCfg)
	if err != nil {
		return fmt.Errorf("creating remote cluster rest mapper: %w", err)
	}
	remoteClient, err := client.New(remoteCfg, client.Options{
		Scheme: remoteScheme,
		Mapper: remoteMapper,
	})
	if err != nil {
		return fmt.Errorf("creating remote cluster client: %w", err)
	}
	remoteCache, err := cache.MultiNamespacedCacheBuilder(remoteNamespaces)(remoteCfg, cache.Options{
		Scheme: remoteScheme,
		Mapper: remoteMapper,
	})
	if err != nil {
		return fmt.Errorf("creating remote cluster cache: %w", err)
	}
	if err := mgr.Add(remoteCache); err != nil {
		return fmt.Errorf("adding remote cluster cache to manager: %w", err)
	}
	remoteCachedClient := &client.DelegatingClient{
		Reader:       remoteCache,
		Writer:       remoteClient,
		StatusClient: remoteClient,
	}

	// Setup Types
	objGVK := schema.GroupVersionKind{
		Kind:    flags.kind,
		Version: flags.version,
		Group:   flags.group,
	}

	// Index
	if err := catapultv1alpha1.RegisterFieldIndexes(
		context.Background(), mgr.GetCache(),
	); err != nil {
		return fmt.Errorf("creating field indexer: %w", err)
	}

	// Register Controllers
	if err = (&controllers.SyncReconciler{
		Log:    ctrl.Log.WithName("controllers").WithName("SyncReconciler"),
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),

		RemoteClusterName: flags.remoteClusterName,
		RemoteClient:      remoteCachedClient,
		RemoteCache:       remoteCache,
		ObjectGVK:         objGVK,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create SyncReconciler controller: %w", err)
	}

	if err = (&controllers.AdoptionReconciler{
		Log:    ctrl.Log.WithName("controllers").WithName("AdoptionReconciler"),
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),

		RemoteClusterName: flags.remoteClusterName,
		RemoteClient:      remoteCachedClient,
		RemoteCache:       remoteCache,
		ObjectGVK:         objGVK,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create AdoptionReconciler controller: %w", err)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("error running manager: %w", err)
	}
	return nil
}
