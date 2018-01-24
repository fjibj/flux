package operator

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	//appslisters "k8s.io/client-go/listers/apps/v1beta2"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/go-kit/kit/log"

	ifv1 "github.com/weaveworks/flux/apis/integrations.flux/v1"
	clientset "github.com/weaveworks/flux/integrations/client/clientset/versioned"
	ifscheme "github.com/weaveworks/flux/integrations/client/clientset/versioned/scheme"
	ifinformers "github.com/weaveworks/flux/integrations/client/informers/externalversions"
	iflister "github.com/weaveworks/flux/integrations/client/listers/integrations/v1"
)

const controllerAgentName = "helm-operator"

const (
	// ChartSynced is used as part of the Event 'reason' when the Chart related to the
	// a FluxHelmResource gets released/updated
	ChartSynced = "ChartSynced"
	// ErrChartSync is used as part of the Event 'reason' when the related Chart related to the
	// a FluxHelmResource fails to be released/updated
	ErrChartSync = "ErrChartSync"

	// MessageChartSynced - the message used for Events when a resource
	// fails to sync due to failing to release the Chart
	MessageChartSynced = "Chart managed by FluxHelmResource released/updated successfully"
	// MessageErrChartSync - the message used for an Event fired when a FluxHelmResource
	// is synced successfully
	MessageErrChartSync = "Chart %q managed by FluxHelmResource failed to be released/updated"
)

// Controller is the operator implementation for FluxHelmResource resources
type Controller struct {
	logger log.Logger
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// fhrclientset is a clientset for our own API group
	fhrclientset clientset.Interface

	// =============================================
	// the deploymentsLister to be perhaps reworked for the Chart releases ???
	//	after syncing based on the relevant CR change
	//deploymentsLister appslisters.DeploymentLister
	// =============================================
	fhrLister iflister.FluxHelmResourceLister

	fhrSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueueCreate workqueue.RateLimitingInterface
	workqueueUpdate workqueue.RateLimitingInterface
	workqueueDelete workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// New returns a new helm-operator
func New(
	logger log.Logger,
	kubeclientset kubernetes.Interface,
	fhrclientset clientset.Interface,
	//kubeInformerFactory kubeinformers.SharedInformerFactory,
	fhrInformerFactory ifinformers.SharedInformerFactory) *Controller {

	// obtain references to shared index informers for the Deployment and FluxHelmResource
	// types.
	//deploymentInformer := kubeInformerFactory.Apps().V1beta2().Deployments()
	fhrInformer := fhrInformerFactory.Integrations().V1().FluxHelmResources()

	// Create event broadcaster
	// Add helm-operator types to the default Kubernetes Scheme so Events can be
	// logged for helm-operator types.
	ifscheme.AddToScheme(scheme.Scheme)
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		logger:        logger,
		kubeclientset: kubeclientset,
		fhrclientset:  fhrclientset,
		//deploymentsLister: deploymentInformer.Lister(),
		//chartsSynced: deploymentInformer.Informer().HasSynced,
		fhrLister: fhrInformer.Lister(),
		fhrSynced: fhrInformer.Informer().HasSynced,

		//---------------------------------------------------------------------------------
		workqueueCreate: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ChartReleaseCreate"),
		workqueueUpdate: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ChartReleaseUpdate"),
		workqueueDelete: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ChartReleaseDelete"),
		recorder:        recorder,
	}

	fmt.Println("===> Setting up event handlers")
	controller.logger.Log("info", "Setting up event handlers")

	// Set up an event handler for when FluxHelmResource resources change
	fhrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(new interface{}) {
			fmt.Printf("\n>>> ADDING a Chart\n")
			fmt.Printf("\t>>> new CRD: %v\n\n", new)
			controller.enqueue(new, "C")
		},
		UpdateFunc: func(old, new interface{}) {

			old, _ = old.(ifv1.FluxHelmResource)
			new, _ = new.(ifv1.FluxHelmResource)
			// if not the same
			fmt.Printf("\n?????????????????????????\ndeciding if to UPDATE a Chart at %s\n", time.Now().String())

			oldBase64, err := serializeToBase64(old.(ifv1.FluxHelmResource))
			if err != nil {
				panic(err)
			}
			newBase64, err := serializeToBase64(new.(ifv1.FluxHelmResource))
			if err != nil {
				panic(err)
			}

			fmt.Printf("old: %#v\n\n", oldBase64)
			fmt.Printf("new: %#v\n\n", newBase64)

			if newBase64 != oldBase64 {
				fmt.Println("===========================>")
				fmt.Printf("\n>>> UPDATING a Chart\n")
				fmt.Printf("\n\\t>>> updating CRD from \n--------\n%v\n   to   \n%v\n--------\n\n", old, new)
				fmt.Println("<==========================>")
				controller.enqueue(new, "U")
			}
		},
		DeleteFunc: func(old interface{}) {
			fmt.Printf("\n>>> DELETING a Chart\n")
			fmt.Printf("\t>>> removing CRD: %v\n\n", old)
			controller.enqueue(old, "D")
		},
	})

	// TODO - deal with possible independent Chart changes
	// -------------------------------------------------------------------

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueueCreate.ShutDown()
	defer c.workqueueUpdate.ShutDown()
	defer c.workqueueDelete.ShutDown()

	c.logger.Log("info", ">>> Starting operator <<<")
	// Wait for the caches to be synced before starting workers
	c.logger.Log("info", "Waiting for informer caches to sync")

	if ok := cache.WaitForCacheSync(stopCh, c.fhrSynced); !ok {
		return fmt.Errorf("error: %s", "failed to wait for caches to sync")
	}

	c.logger.Log("info", "=== Starting workers ===")
	// Launch two workers to process FluxHelmResource resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runCreateWorker, time.Second, stopCh)
		go wait.Until(c.runUpdateWorker, time.Second, stopCh)
		go wait.Until(c.runDeleteWorker, time.Second, stopCh)
	}

	<-stopCh
	c.logger.Log("info", "=== Stopping workers ===")

	return nil
}

func serializeToBase64(obj ifv1.FluxHelmResource) (string, error) {
	b := bytes.Buffer{}
	err := gob.NewEncoder(&b).Encode(obj)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b.Bytes()), nil
}

// runXWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// specific workqueue.
func (c *Controller) runCreateWorker() {
	for c.processNextWorkItem("C") {
	}
}
func (c *Controller) runUpdateWorker() {
	for c.processNextWorkItem("U") {
	}
}
func (c *Controller) runDeleteWorker() {
	for c.processNextWorkItem("D") {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem(action string) bool {
	fmt.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	c.logger.Log("info", fmt.Sprintf("!!! in processNextWorkItem: [ACTION %#v]", action))

	var wq workqueue.RateLimitingInterface
	switch action {
	case "C":
		wq = c.workqueueCreate
	case "U":
		wq = c.workqueueUpdate
	case "D":
		wq = c.workqueueDelete
	}

	obj, shutdown := wq.Get()
	c.logger.Log("info", fmt.Sprintf("\t\t\t---> processing item %#v]\n", obj))

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}, action string) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We must call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer wq.Done(obj)

		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form "namespace/fhr name". We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date than when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			wq.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// FluxHelmResource resource to be synced.
		// If the sync failed, then we return while the item will get requeued
		if err := c.syncHandler(key, action); err != nil {
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		wq.Forget(obj)
		c.logger.Log("info", fmt.Sprintf("\t\t *** Successfully synced '%s'", key))
		c.logger.Log("info", fmt.Sprintf("$$$$$ WORK QUEUE\n---\n%#v\n---\nlength: %d\n\n", wq, wq.Len()))
		c.logger.Log("info", "... still here 1")

		return nil
	}(obj, action)

	c.logger.Log("info", "... still here 2")
	fmt.Println("X~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~\n")

	if err != nil {
		c.logger.Log("CRASH", fmt.Sprintf("\t\t *** %#v", err))

		runtime.HandleError(err)
		c.logger.Log("info", "... still here 3")
		return true
	}
	c.logger.Log("info", "... still here 4")
	fmt.Println("X+++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++\n")

	return true
}

// syncHandler acts according to the action
//------------------------------------------------------------------------
func (c *Controller) syncHandler(key string, action string) error {
	c.logger.Log("info", fmt.Sprintf("!!! in syncHandler - for action %s", action))

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	fhr, err := c.fhrLister.FluxHelmResources(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("fluxhelmresource '%s' in work queue no longer exists", key))
			return nil
		}
		return err
	}

	customs := fhr.Spec.Customizations

	//fmt.Printf("=== %#v\n\n", customs)

	// Sanity check of input - needed ?
	if customs != nil {
		// TODO collect all empty values => return an error with the info
		for _, cu := range customs {
			if cu.Value == "" {
				// We choose to absorb the error here as the worker would requeue the
				// resource otherwise. Instead, the next time the resource is updated
				// the resource will be queued again.

				// TODO send error to upstream (through RPC server)  instead of absorbing and skip the syncing of this fhr
				runtime.HandleError(fmt.Errorf("%s: customization value must be specified [%s]", key, cu.Name))

				fmt.Printf("=== %s: customization value must be specified [%s]\n\n", key, cu.Name)
				c.recorder.Event(fhr, corev1.EventTypeNormal, ErrChartSync, MessageErrChartSync)

				return nil
			}
		}
	}

	// Now do something with the relevant Chart
	// ----------------------------------------
	// Get the release associated with this Chart:
	// a) helm operator created release name
	// b) release from helm release listing

	// Release does not exist => release the Chart and specify the release name (namespace:Chart name)
	// Release exists => update the Chart release

	// Finally, we update the status block of the FluxHelmResource resource to reflect the
	// current state of the world
	/*
		err = c.updateFluxHelmResourceStatus(fhr, chartRelease)
		if err != nil {
			return err
		}
	*/

	c.recorder.Event(fhr, corev1.EventTypeNormal, ChartSynced, MessageChartSynced)
	return nil
}

func (c *Controller) updateFluxHelmResourceStatus(fhr *ifv1.FluxHelmResource, deployment *appsv1beta2.Deployment) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance

	/*
		fhrCopy := fhr.DeepCopy()
		fhrCopy.Status.AvailableReplicas = deployment.Status.AvailableReplicas
		// Until #38113 is merged, we must use Update instead of UpdateStatus to
		// update the Status block of the FluxHelmResource resource. UpdateStatus will not
		// allow changes to the Spec of the resource, which is ideal for ensuring
		// nothing other than resource status has been updated.
		_, err := c.fhrclientset.SamplecontrollerV1alpha1().FluxHelmResources(fhr.Namespace).Update(fhrCopy)
		return err
	*/
	return nil
}

func getCacheKey(obj interface{}) (string, error) {
	fmt.Println("=== in getCacheKey ===")

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		fmt.Printf("*** ERROR %#v\n", err)
		return "", err
	}
	return key, nil
}

// enqueueFluxHelmResource takes a FluxHelmResource resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should not be
// passed resources of any type other than FluxHelmResource.
func (c *Controller) enqueue(obj interface{}, action string) {
	fmt.Printf("=== in enqueue for %s ===\n", action)

	var key string
	var err error
	if key, err = getCacheKey(obj); err != nil {
		return
	}
	fmt.Printf("*** Cache key is %s\n", key)

	var wq workqueue.RateLimitingInterface
	switch action {
	case "C":
		wq = c.workqueueCreate
	case "U":
		wq = c.workqueueUpdate
	case "D":
		wq = c.workqueueDelete
	}
	wq.AddRateLimited(key)
}

func (c *Controller) deleteChart(obj interface{}) error {
	var key string
	var err error
	if key, err = getCacheKey(obj); err != nil {
		return nil
	}

	parts := strings.Split(key, "/")
	fhr, err := c.fhrLister.FluxHelmResources(parts[0]).Get(parts[1])
	if err != nil {
		// The FluxHelmResource resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("fluxhelmresource '%s' in work queue no longer exists", key))
			return nil
		}
		return err
	}
	chart := fhr.Name
	fmt.Printf("\t@@@ DELETED Chart %s\n", chart)

	return nil
}

//func newChartRelease(fhr *ifv1.FluxHelmResource) *appsv1beta2.Deployment {
func newChartRelease(fhr *ifv1.FluxHelmResource) {
	fmt.Printf("\t@@@  Released new chart for %#v\n\n", fhr.Name)
}

func chartReleaseUpdate(fhr *ifv1.FluxHelmResource) {
	fmt.Printf("\t@@@ Updated release for new chart for %#v\n\n", fhr.Name)
}
