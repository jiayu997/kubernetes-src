package ingress_controller_demo1

import (
	"k8s.io/client-go/kubernetes"
	coreV1 "k8s.io/client-go/listers/core/v1"
	networkV1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/util/workqueue"
)

type Controller struct {
	client        *kubernetes.Clientset
	serviceLister coreV1.ServiceLister
	ingressLister networkV1.IngressLister
	queue         workqueue.RateLimitingInterface
}
