module github.com/gardener/gardener

go 1.16

require (
	github.com/Masterminds/semver v1.5.0
	github.com/Masterminds/sprig v2.22.0+incompatible
	github.com/ahmetb/gen-crd-api-reference-docs v0.2.0
	github.com/coreos/go-systemd/v22 v22.1.0
	github.com/envoyproxy/go-control-plane v0.9.7-0.20200730005029-803dd64f0468
	github.com/gardener/component-spec/bindings-go v0.0.33
	github.com/gardener/dependency-watchdog v0.6.1-0.20210623112844-96f73d5dc311
	github.com/gardener/etcd-druid v0.5.0
	github.com/gardener/external-dns-management v0.7.18
	github.com/gardener/gardener-resource-manager v0.25.0
	github.com/gardener/gardener-resource-manager/api v0.0.0-00010101000000-000000000000
	github.com/gardener/hvpa-controller v0.3.1
	github.com/gardener/landscaper/apis v0.7.0
	github.com/gardener/machine-controller-manager v0.33.0
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.4.0
	github.com/go-logr/zapr v0.4.0
	github.com/go-openapi/spec v0.19.5
	github.com/gogo/protobuf v1.3.2
	github.com/golang/mock v1.6.0
	github.com/googleapis/gnostic v0.5.5
	github.com/hashicorp/go-multierror v1.1.0
	github.com/huandu/xstrings v1.3.2
	github.com/mholt/archiver v3.1.1+incompatible
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.13.0
	github.com/prometheus/client_golang v1.11.0
	github.com/robfig/cron v1.2.0
	github.com/sirupsen/logrus v1.7.0
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.0
	github.com/texttheater/golang-levenshtein v0.0.0-20191208221605-eb6844b05fc6
	go.uber.org/zap v1.17.0
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83
	golang.org/x/lint v0.0.0-20201208152925-83fdc39ff7b5
	golang.org/x/time v0.0.0-20210611083556-38a9dc6acbc6
	gomodules.xyz/jsonpatch/v2 v2.2.0
	gonum.org/v1/gonum v0.8.2
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
	istio.io/api v0.0.0-20210520012029-891c0c12abfd
	istio.io/client-go v1.10.1
	k8s.io/api v0.21.2
	k8s.io/apiextensions-apiserver v0.21.2
	k8s.io/apimachinery v0.21.2
	k8s.io/apiserver v0.21.2
	k8s.io/autoscaler v0.0.0-20190805135949-100e91ba756e
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/cluster-bootstrap v0.21.2
	k8s.io/code-generator v0.21.2
	k8s.io/component-base v0.21.2
	k8s.io/gengo v0.0.0-20201214224949-b6c5ce23f027
	k8s.io/helm v2.16.1+incompatible
	k8s.io/klog v1.0.0
	k8s.io/klog/v2 v2.9.0
	k8s.io/kube-aggregator v0.21.2
	k8s.io/kube-openapi v0.0.0-20210305001622-591a79e4bda7 // keep this value in sync with k8s.io/apiserver
	k8s.io/kubelet v0.21.2
	k8s.io/metrics v0.21.2
	k8s.io/utils v0.0.0-20210527160623-6fdb442a123b
	sigs.k8s.io/controller-runtime v0.9.1
	sigs.k8s.io/controller-runtime/tools/setup-envtest v0.0.0-20210609022947-fbf50b04fe17
	sigs.k8s.io/controller-tools v0.6.0
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/emicklei/go-restful => github.com/emicklei/go-restful v2.9.5+incompatible // keep this value in sync with k8s.io/apiserver
	github.com/envoyproxy/go-control-plane => github.com/envoyproxy/go-control-plane v0.9.4
	github.com/gardener/gardener-resource-manager/api => github.com/gardener/gardener-resource-manager/api v0.25.0
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v1.11.0 // keep this value in sync with sigs.k8s.io/controller-runtime
	google.golang.org/grpc => google.golang.org/grpc v1.27.1 // keep this value in sync with k8s.io/apiserver
	k8s.io/api => k8s.io/api v0.21.2
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.21.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.21.2
	k8s.io/apiserver => k8s.io/apiserver v0.21.2
	k8s.io/client-go => k8s.io/client-go v0.21.2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.21.2
	k8s.io/code-generator => k8s.io/code-generator v0.21.2
	k8s.io/component-base => k8s.io/component-base v0.21.2
	k8s.io/helm => k8s.io/helm v2.13.1+incompatible
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.21.2
)
