package scheme

import (
	"bytes"
	"fmt"
	"io"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"log"
)

import (
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

func int32Ptr(i int32) *int32 { return &i }

func main() {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "demo-deployment",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "demo",
				},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "demo",
					},
				},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{
							Name:  "web",
							Image: "nginx:1.12",
							Ports: []apiv1.ContainerPort{
								{
									Name:          "http",
									Protocol:      apiv1.ProtocolTCP,
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}
	// 1.
	negotiator := runtime.NewClientNegotiator(scheme.Codecs.WithoutConversion(), schema.GroupVersion{Group: "apps", Version: "v1"})
	// 2.
	encoder, err := negotiator.Encoder("application/json", nil)
	if err != nil {
		log.Fatal("初始化eecoder失败", err)
	}

	out := bytes.NewBuffer(nil)
	// 3.
	if err := encoder.Encode(deployment, out); err != nil {
		log.Fatal("编码失败", err)
	}
	data, err := io.ReadAll(out)
	if err != nil {
		log.Fatal("读取失败", err)
	}
	fmt.Println(string(data))
	// 4.
	decoder, err := negotiator.Decoder("application/json", nil)
	if err != nil {
		log.Fatal("初始化decoder失败", err)
	}
	// 5.
	obj, gvk, err := decoder.Decode(data, nil, nil)
	if err != nil {
		log.Fatal("解码失败", err)
	}
	deploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		log.Fatal("解码不符合预期")
	}
	fmt.Println(deploy.Name)
	fmt.Printf("%#v\n", gvk)
}

// 输出结果
// {"kind":"Deployment","apiVersion":"apps/v1","metadata":{"name":"demo-deployment","creationTimestamp":null},"spec":{"replicas":2,"selector":{"matchLabels":{"app":"demo"}},"template":{"metadata":{"creationTimestamp":null,"labels":{"app":"demo"}},"spec":{"containers":[{"name":"web","image":"nginx:1.12","ports":[{"name":"http","containerPort":80,"protocol":"TCP"}],"resources":{}}]}},"strategy":{}},"status":{}}
//demo-deployment
// &schema.GroupVersionKind{Group:"apps", Version:"v1", Kind:"Deployment"}
