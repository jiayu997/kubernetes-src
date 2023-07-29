package demo1

import (
	"context"
	"errors"
	"fmt"
	flag "github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
	"testing"
)

func getNewClientSet() (*kubernetes.Clientset, error) {
	var err error
	var config *rest.Config
	var kubeconfig *string

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "")
	}

	if config, err = rest.InClusterConfig(); err != nil {
		if config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig); err != nil {
			return nil, errors.New("can't init by cluster mode")
		}
	}

	// create clientset
	if clientset, err := kubernetes.NewForConfig(config); err != nil {
		return nil, errors.New("can't init by kubeconfig")
	} else {
		return clientset, nil
	}

}

func TestGetAllDeployments(t *testing.T) {
	client, err := getNewClientSet()
	if err != nil {
		return
	}
	// client.AppsV1() = staging/src/k8s.io/client-go/kubernetes/typed/apps/v1/apps_client.go AppsV1Client
	// client.AppsV1().Deployments() = staging/src/k8s.io/client-go/kubernetes/typed/apps/v1/apps_client.go newDeployments(c, namespace)
	deployments, _ := client.AppsV1().Deployments("").List(context.TODO(), metav1.ListOptions{})

	for _, deployment := range deployments.Items {
		fmt.Println(deployment.Name, deployment.Namespace)
	}
}
