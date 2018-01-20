package main

import (
	"sync"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	"fmt"
	"os"
	"os/signal"

	"github.com/go-kit/kit/log"
	/*
		"github.com/coreos/etcd-operator/pkg/client"
		"github.com/coreos/etcd-operator/pkg/controller"
		"github.com/coreos/etcd-operator/pkg/debug"
		"github.com/coreos/etcd-operator/pkg/util/constants"
		"github.com/coreos/etcd-operator/pkg/util/k8sutil"
		"github.com/coreos/etcd-operator/pkg/util/probe"
		"github.com/coreos/etcd-operator/pkg/util/retryutil"
		"github.com/coreos/etcd-operator/version"
	*/ //	"github.com/prometheus/client_golang/prometheus"
	//	"github.com/sirupsen/logrus"

	//"github.com/weaveworks/flux/git"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ifinformers "github.com/weaveworks/flux/integrations/client/informers/externalversions"
	//"github.com/weaveworks/flux/integrations/helm/operator/operator"

	clientset "github.com/weaveworks/flux/integrations/client/clientset/versioned"
	"github.com/weaveworks/flux/integrations/helm/chart"
	helmclient "github.com/weaveworks/flux/integrations/helm/client"
	"github.com/weaveworks/flux/integrations/helm/operator"
	"github.com/weaveworks/flux/ssh"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	fs      *pflag.FlagSet
	err     error
	logger  log.Logger
	kubectl string

	kubeconfig          *string
	master              *string
	crdPollInterval     *time.Duration
	eventHandlerWorkers *uint

	customKubectl *string
	gitURL        *string
	gitBranch     *string
	gitPath       *string

	k8sSecretName            *string
	k8sSecretVolumeMountPath *string
	k8sSecretDataKey         *string
	sshKeyBits               ssh.OptionalValue
	sshKeyType               ssh.OptionalValue

	name       *string
	listenAddr *string
	gcInterval *time.Duration
)

func init() {
	// Flags processing
	fs = pflag.NewFlagSet("default", pflag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "DESCRIPTION\n")
		fmt.Fprintf(os.Stderr, "  helm-operator is a Kubernetes operator for Helm integration into flux.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "FLAGS\n")
		fs.PrintDefaults()
	}

	kubeconfig = fs.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	master = fs.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")

	crdPollInterval = fs.Duration("crd-poll-interval", 5*time.Minute, "Period at which to check for custom resources")
	eventHandlerWorkers = fs.Uint("event-handler-workers", 2, "Number of workers processing events for Flux-Helm custom resources")

	customKubectl = fs.String("kubernetes-kubectl", "", "Optional, explicit path to kubectl tool")
	gitURL = fs.String("git-url", "", "URL of git repo with Kubernetes manifests; e.g., git@github.com:weaveworks/flux-example")
	gitBranch = fs.String("git-branch", "master", "branch of git repo to use for Kubernetes manifests")
	gitPath = fs.String("git-path", "", "path within git repo to locate Kubernetes manifests (relative path)")

	// k8s-secret backed ssh keyring configuration
	k8sSecretName = fs.String("k8s-secret-name", "flux-git-deploy", "Name of the k8s secret used to store the private SSH key")
	k8sSecretVolumeMountPath = fs.String("k8s-secret-volume-mount-path", "/etc/fluxd/ssh", "Mount location of the k8s secret storing the private SSH key")
	k8sSecretDataKey = fs.String("k8s-secret-data-key", "identity", "Data key holding the private SSH key within the k8s secret")
	// SSH key generation
	sshKeyBits = optionalVar(fs, &ssh.KeyBitsValue{}, "ssh-keygen-bitsintegrations/", "-b argument to ssh-keygen (default unspecified)")
	sshKeyType = optionalVar(fs, &ssh.KeyTypeValue{}, "ssh-keygen-type", "-t argument to ssh-keygen (default unspecified)")

}

type RevisionPatch struct {
	Revision string
}
type StatusPatch struct {
	Status RevisionPatch
}

func main() {

	fs.Parse(os.Args)

	// Set up logging
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}
	// ----------------------------------------------------------------------

	// Set up shutdown
	errc := make(chan error)

	// Shutdown trigger for goroutines
	shutdown := make(chan struct{})
	shutdownWg := &sync.WaitGroup{}

	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errc <- fmt.Errorf("%s", <-c)
	}()

	defer func() {
		// wait until stopping
		logger.Log("exiting...", <-errc)
		close(shutdown)
		shutdownWg.Wait()
	}()
	// ----------------------------------------------------------------------

	// Check if the FluxHelmResources exist in the cluster
	//		later on add a check that the CRD itself exists and creat it if not

	logger.Log("component", "helm-operator", "info", "!!! I am functional! !!!")

	// get CRD clientset
	//------------------
	// set up cluster configuration
	cfg, err := clientcmd.BuildConfigFromFlags(*master, *kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	ts, err := kubeClient.CoreV1().Services("kube-system").Get("tiller-deploy", metav1.GetOptions{})
	fmt.Printf("\n-------------\n>>> TILLER SERVICE ALL=%#v\n-------------\n", ts.Spec)

	fmt.Printf("\n-------------\n>>> TILLER SERVICE IP=%#v\n-------------\n", ts.Spec.ClusterIP)
	if err != nil {
		panic(err)
	}

	ports := ts.Spec.Ports
	fmt.Printf("\n-------------\n>>> TILLER SERVICE PORTS=%#v\n-------------\n", ports)

	ip := ts.Spec.ClusterIP
	//port := "44134"
	port := ts.Spec.Ports[0].Port
	fmt.Printf("\n-------------\n>>> TILLER SERVICE PORT=%#v\n-------------\n", port)

	opts := helmclient.Options{
		IP:   ip,
		Port: fmt.Sprintf("%v", port),
	}

	hc, err := helmclient.New(kubeClient, opts)
	if err != nil {
		panic(err)
	}

	ch := chart.Chart{
		Client: *hc,
	}

	ch.GetReleases()

	/*
		kss, err := kubeClient.CoreV1().Services("kube-system").List(metav1.ListOptions{})
		if err != nil {
			glog.Fatalf("Error getting services: %s", err.Error())
		}
		for i, s := range kss.Items {
			fmt.Printf(">>> services: %d - %#v\n\n", i, s)
		}
	*/

	ifClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building integrations clientset: %v", err)
	}

	//	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kube, time.Second*30)
	ifInformerFactory := ifinformers.NewSharedInformerFactory(ifClient, time.Second*30)
	go ifInformerFactory.Start(shutdown)

	//	go func(logger log.Logger) {
	//		for {
	/*
		// create CRD:
		/*
		A CRD manifest is loaded before helm-operator starts
	*/
	//n := 11111
	for {
		list, err := ifClient.IntegrationsV1().FluxHelmResources("kube-system").List(metav1.ListOptions{})
		fmt.Printf("\n>>> FOUND %v items\n\n", len(list.Items))
		if err != nil {
			glog.Errorf("Error listing all fluxhelmresources: %v", err)
			//time.Sleep(1 * time.Minute)
			//continue
			os.Exit(1)
		}

		for _, fhr := range list.Items {
			fmt.Println("=============== START OF PATCHING ================")
			fmt.Println("-----------------------------------------------------")
			fmt.Printf("fluxhelmresource %s:\n\n%#v\n", fhr.Name, fhr)
			fmt.Println("-----------------------------------------------------")

			fmt.Printf("fluxhelmresource %s for chart path %q and release name [%s] with customizations %#v\n", fhr.Name, fhr.Spec.ChartGitPath, fhr.Spec.ReleaseName, fhr.Spec.Customizations)

			fmt.Printf("\t\t>>> found %v parameters\n", len(fhr.Spec.Customizations))

			for _, cp := range fhr.Spec.Customizations {
				fmt.Printf("\t\t * customization with \n\t\tname %q\n\t\tvalue %q\n", cp.Name, cp.Value)
			}

			/*
				n = n + 10
				statusPatch := StatusPatch{
					Status: RevisionPatch{strconv.Itoa(n)},
				}

				data, err := json.Marshal(statusPatch)

				fmt.Println(">>>> ", string(data), " <<<<")

				if err != nil {
					logger.Log("E R R O R", err.Error())
				}
				newFhr, err := ifClient.IntegrationsV1().FluxHelmResources("kube-system").Patch(fhr.Name, types.StrategicMergePatchType, data)
				if err != nil {
					logger.Log("E R R O R", err.Error())
					continue
				}
				fmt.Printf("\t\tS U C C E S S - patched to %#v\n\n", newFhr)

				fmt.Println("============== END OF PATCHING =================")
			*/

			time.Sleep(15 * time.Second)

		}

		time.Sleep(30 * time.Second)
	}

	//		}
	//	}(log.With(logger, "fhr loop", "testing"))

	//
	opr := operator.New(log.With(logger, "component", "helm-operator"), kubeClient, ifClient, ifInformerFactory)

	if err = opr.Run(2, shutdown); err != nil {
		glog.Fatalf("Error running controller: %s", err.Error())
	}

}

// Helper functions
func optionalVar(fs *pflag.FlagSet, value ssh.OptionalValue, name, usage string) ssh.OptionalValue {
	fs.Var(value, name, usage)
	return value
}
