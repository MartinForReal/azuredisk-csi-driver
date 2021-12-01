module sigs.k8s.io/azuredisk-csi-driver

go 1.16

require (
	github.com/Azure/azure-sdk-for-go v55.8.0+incompatible
	github.com/Azure/go-autorest/autorest v0.11.22
	github.com/Azure/go-autorest/autorest/adal v0.9.16
	github.com/Azure/go-autorest/autorest/date v0.3.0
	github.com/Azure/go-autorest/autorest/to v0.4.0
	github.com/container-storage-interface/spec v1.5.0
	github.com/golang/mock v1.6.0
	github.com/golang/protobuf v1.5.2
	github.com/kubernetes-csi/csi-lib-utils v0.7.0
	github.com/kubernetes-csi/csi-proxy/client v1.0.1
	github.com/kubernetes-csi/external-snapshotter/v2 v2.0.0-20200617021606-4800ca72d403
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.16.0
	github.com/pborman/uuid v1.2.0
	github.com/pelletier/go-toml v1.9.3
	github.com/prometheus/client_golang v1.11.0
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.0
	golang.org/x/net v0.0.0-20210520170846-37e1c6afe023
	google.golang.org/grpc v1.41.0
	google.golang.org/protobuf v1.26.0
	k8s.io/api v0.22.4
	k8s.io/apimachinery v0.22.4
	k8s.io/client-go v0.22.4
	k8s.io/cloud-provider v0.22.4
	k8s.io/code-generator v0.22.4
	k8s.io/component-base v0.22.4
	k8s.io/component-helpers v0.22.4
	k8s.io/klog v1.0.0
	k8s.io/klog/v2 v2.10.0
	k8s.io/kube-scheduler v0.22.4
	k8s.io/kubernetes v1.22.3
	k8s.io/mount-utils v0.22.4
	k8s.io/utils v0.0.0-20210819203725-bdf08cb9a70a
	sigs.k8s.io/cloud-provider-azure v0.7.4
	sigs.k8s.io/controller-runtime v0.10.1
	sigs.k8s.io/controller-tools v0.5.0
	sigs.k8s.io/yaml v1.3.0
)

replace (
	github.com/niemeyer/pretty => github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e
	go.etcd.io/etcd => go.etcd.io/etcd v0.0.0-20200410171415-59f5fb25a533
	k8s.io/api => k8s.io/api v0.22.3
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.22.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.22.3
	k8s.io/apiserver => k8s.io/apiserver v0.22.3
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.22.3
	k8s.io/client-go => k8s.io/client-go v0.22.3
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.22.3
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.22.3
	k8s.io/code-generator => k8s.io/code-generator v0.22.3
	k8s.io/component-base => k8s.io/component-base v0.22.3
	k8s.io/component-helpers => k8s.io/component-helpers v0.22.3
	k8s.io/controller-manager => k8s.io/controller-manager v0.22.3
	k8s.io/cri-api => k8s.io/cri-api v0.22.3
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.0.0-20200530124324-08bf6a63b59d
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.22.3
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.22.3
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20201113171705-d219536bb9fd
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.22.3
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.22.3
	k8s.io/kubectl => k8s.io/kubectl v0.22.3
	k8s.io/kubelet => k8s.io/kubelet v0.22.3
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.22.3
	k8s.io/metrics => k8s.io/metrics v0.22.3
	k8s.io/mount-utils => k8s.io/mount-utils v0.22.3
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.22.3
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.22.3
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.22.3
	k8s.io/sample-controller => k8s.io/sample-controller v0.22.3

	sigs.k8s.io/cloud-provider-azure => github.com/edreed/cloud-provider-azure v0.0.0-20211111214527-f012c48a31cc
)
