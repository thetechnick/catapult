# Catapult Concept

```yaml
apiGroup: catapult.thetechnick.ninja/v1alpha1
kind: Consumer
metadata:
  name: "consumer-01"
  labels: {}
spec:
  subjects:
  - kind: ServiceAccount
    name: consumer-01-token-01
    apiGroup: ""
status:
  phase: Ready
```

---

```yaml
apiGroup: catapult.thetechnick.ninja/v1alpha1
kind: 
metadata:
  name: ""
spec: {}
status:
  phase: Ready
  conditions:
  - type: ""
    status: "True"
    message: ""
    reason: ""
```

## Goals

- specify available CRDs

---

```yaml
apiGroup: catapult.thetechnick.ninja/v1alpha1
kind: ServiceCluster
metadata:
  name: database-cluster
kubeconfigSecret:
  name: database-cluster-kubeconfig
```

```yaml
apiGroup: catapult.thetechnick.ninja/v1alpha1
kind: RemoteAPI
metadata:
  name: redis
spec:
  serviceCluster:
    name: database-cluster
  crd:
    name: redis.redis.io
status:
  phase: Ready
```
