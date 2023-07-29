package reflector

import (
	"context"
	"errors"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
	"testing"
	"time"
)

var NeverStop <-chan struct{} = make(chan struct{})

type testLw struct {
	ListFunc  func(options v1.ListOptions) (runtime.Object, error)
	WatchFunc func(options v1.ListOptions) (watch.Interface, error)
}

func (t *testLw) List(options metav1.ListOptions) (runtime.Object, error) {
	return t.ListFunc(options)
}

func (t *testLw) Watch(options metav1.ListOptions) (watch.Interface, error) {
	return t.WatchFunc(options)
}

// add user custom list options
func testTeakListOption(options *metav1.ListOptions, resourceType string) {
	switch resourceType {
	case "Deployment":
		options.TypeMeta.APIVersion = "apps/v1"
		options.TypeMeta.Kind = "Deployment"
	case "Pod":
		options.TypeMeta.APIVersion = "v1"
		options.TypeMeta.Kind = "Pod"
	}
	options.Limit = 500
}

func testNewClientSet() (kubernetes.Interface, error) {
	var err error
	var config *rest.Config
	var kubeconfig string

	// get kubeconfig file path
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	} else {
		kubeconfig = ""
	}

	// get config
	if config, err = rest.InClusterConfig(); err != nil {
		if config, err = clientcmd.BuildConfigFromFlags("", kubeconfig); err != nil {
			return nil, errors.New("can't init by kubeconfig")
		}
	}

	// create clientset
	if clientset, err := kubernetes.NewForConfig(config); err != nil {
		return nil, errors.New("can't init clientset by kubeconfig")
	} else {
		return clientset, nil
	}
}

func TestNewReflectorWithNoIndexerDeployment(t *testing.T) {
	// create client set
	client, err := testNewClientSet()
	if err != nil {
		t.Errorf(err.Error())
	}

	// create lister/watcher
	lw := testLw{
		ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
			// set user custom filter options
			testTeakListOption(&options, "Deployment")
			return client.AppsV1().Deployments(metav1.NamespaceAll).List(context.TODO(), options)
		},
		WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
			// set user custom filter options
			testTeakListOption(&options, "Deployment")
			return client.AppsV1().Deployments(metav1.NamespaceAll).Watch(context.TODO(), options)
		},
	}

	// new delta fifo
	fifo := cache.NewDeltaFIFOWithOptions(cache.DeltaFIFOOptions{
		KeyFunction: cache.MetaNamespaceKeyFunc,
		// indexer = nil
		KnownObjects: nil,
	})

	// new reflector
	reflector := cache.NewReflector(&lw, &appsv1.Deployment{}, fifo, time.Second*30)

	// get deployments from apiserver
	go reflector.Run(NeverStop)

	for {
		// get deployment from delta fifo
		obj, err := fifo.Pop(func(i interface{}) error {
			//fmt.Println("delta fifo pop , here we will do somethings that you want to do")
			return nil
		})

		if err != nil {
			t.Error(err.Error())
		}

		deltas, ok := obj.(cache.Deltas)
		if !ok {
			t.Error("delta fifo assert failed")
		}

		for _, delta := range deltas {
			ptDeployment, ok := delta.Object.(*appsv1.Deployment)
			if !ok {
				t.Error("delta object convert deployment failed")
			}
			t.Logf("event type: %s namespace: %s deployment_name: %s", delta.Type, ptDeployment.Namespace, ptDeployment.Name)
		}
	}
}
