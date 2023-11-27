package ingress_controller_demo1

import (
	coreV1 "k8s.io/api/core/v1"
	networkV1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func constructIngress(service *coreV1.Service) *networkV1.Ingress {
	var ingressServicePort int32 = 80
	ingressClassName := "nginx"
	ingressPathType := networkV1.PathTypePrefix
	ingress := networkV1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.Name,
			Namespace: service.Namespace,
			Labels:    service.GetLabels(),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(service, coreV1.SchemeGroupVersion.WithKind("Service")),
			},
		},
		Spec: networkV1.IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []networkV1.IngressRule{
				networkV1.IngressRule{
					Host: "example.com",
					IngressRuleValue: networkV1.IngressRuleValue{
						HTTP: &networkV1.HTTPIngressRuleValue{
							Paths: []networkV1.HTTPIngressPath{
								networkV1.HTTPIngressPath{
									Path:     "/",
									PathType: &ingressPathType,
									Backend: networkV1.IngressBackend{
										Service: &networkV1.IngressServiceBackend{
											Name: service.Name,
											Port: networkV1.ServiceBackendPort{
												Number: ingressServicePort,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return &ingress
}
