1. lease保存信息
```yaml
#➜  ~ kubectl get lease example -n default -o yaml
apiVersion: coordination.k8s.io/v1
kind: Lease
metadata:
  creationTimestamp: "2020-02-15T11:56:37Z"
  name: example
  namespace: default
  resourceVersion: "210675"
  selfLink: /apis/coordination.k8s.io/v1/namespaces/default/leases/example
  uid: a3470a06-6fc3-42dc-8242-9d6cebdf5315
spec:
  acquireTime: "2020-02-15T12:01:41.476971Z"//获得锁时间
  holderIdentity: "2"//持有锁进程的标识
  leaseDurationSeconds: 60//lease租约
  leaseTransitions: 1//leader更换次数
  renewTime: "2020-02-15T12:05:37.134655Z"//更新租约的时间
```