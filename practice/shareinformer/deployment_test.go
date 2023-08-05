package shareinformer

import (
	"context"
	"errors"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
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

type ListWatch struct {
	ListFunc  func(options metav1.ListOptions) (runtime.Object, error)
	WatchFunc func(options metav1.ListOptions) (watch.Interface, error)
}

func (lw *ListWatch) List(options metav1.ListOptions) (runtime.Object, error) {
	return lw.ListFunc(options)
}

func (lw *ListWatch) Watch(options metav1.ListOptions) (watch.Interface, error) {
	return lw.WatchFunc(options)
}

func labelRequirementMethod1(options *metav1.ListOptions) {
	equalRequirement, err := labels.NewRequirement("app", selection.Equals, []string{"postgres"})
	if err != nil {
		panic(err)
	}
	selector := labels.NewSelector().Add(*equalRequirement)
	options.LabelSelector = selector.String()
}

func labelParseMethod2(options *metav1.ListOptions) {
	// 语法
	parsedSelector, err := labels.Parse("bind-service=none,app notin (not_exists)")
	if err != nil {
		panic(err)
	}
	options.LabelSelector = parsedSelector.String()
}

func labelSelectorFromSetMethod3(options *metav1.ListOptions) {
	setSelector := labels.SelectorFromSet(labels.Set(map[string]string{"app": "postgres"}))

	options.LabelSelector = setSelector.String()
}

func labelSelectorMethod4(options *metav1.ListOptions) {
	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "postgres"},
	}

	convertSelector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		panic(err)
	}
	options.LabelSelector = convertSelector.String()
}

func DeploymentTweakListOptionFilter(options *metav1.ListOptions) {
	options.Limit = 20
	// 创建labelSelector方法
	//1. 创建NewRequirement对象，加入labels.Selector
	//2. labels.Parse方法，将字符串转为labels.Selector对象(最简单)
	//3. labels.SelectorFromSet方法，用map生成labels.Selector对象
	//4. metav1.LabelSelectorAsSelector方法，将LabelSelector对象转为labels.Selector对象
	labelSelectorFromSetMethod3(options)
}

func newClientSet() (kubernetes.Interface, error) {
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

func TestDeploymentShareIndexInformer(t *testing.T) {
	client, err := newClientSet()
	if err != nil {
		t.Error(err)
	}

	// create list and watch
	lw := ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			// set user define options
			//DeploymentTweakListOptionFilter(&options)
			return client.AppsV1().Deployments(metav1.NamespaceAll).List(context.TODO(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			//DeploymentTweakListOptionFilter(&options)
			return client.AppsV1().Deployments(metav1.NamespaceAll).Watch(context.TODO(), options)
		},
	}

	// create shareindexinformer
	shareindexinformer := cache.NewSharedIndexInformer(&lw, &appsv1.Deployment{}, time.Second*15, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})

	// add event resource handle and start processorListener and init sharedProcessor
	shareindexinformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			deployment, ok := obj.(*appsv1.Deployment)
			if !ok {
				return
			}
			t.Logf("Add namespace: %s deployment: %s", deployment.Namespace, deployment.Name)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			newDeployment, ok := newObj.(*appsv1.Deployment)
			if !ok {
				return
			}
			t.Logf("Update namespace: %s deployment: %s", newDeployment.Namespace, newDeployment.Name)
		},
		DeleteFunc: func(obj interface{}) {
			deployment, ok := obj.(*appsv1.Deployment)
			if !ok {
				return
			}
			t.Logf("Delete namespace: %s deployment: %s", deployment.Namespace, deployment.Name)
		},
	})

	// start shareindexinformer
	ch := make(chan struct{})

	t.Logf("start shareindexinformer")
	shareindexinformer.Run(ch)

	<-ch
}
