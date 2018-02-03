package operator

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
	iflister "github.com/weaveworks/flux/integrations/client/listers/integrations.flux/v1"
	chartrelease "github.com/weaveworks/flux/integrations/helm/release"
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
	MessageErrChartSync = "Chart %s managed by FluxHelmResource failed to be released/updated"
)

// Controller is the operator implementation for FluxHelmResource resources
type Controller struct {
	logger log.Logger
	// 		kubeclientset is a standard kubernetes clientset
	// kubeclientset kubernetes.Interface
	// 		fhrclientset is a clientset for our own API group
	// fhrclientset clientset.Interface

	fhrLister iflister.FluxHelmResourceLister
	fhrSynced cache.InformerSynced

	release *chartrelease.Release

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	releaseWorkqueue workqueue.RateLimitingInterface

	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// New returns a new helm-operator
func New(
	logger log.Logger,
	kubeclientset kubernetes.Interface,
	fhrclientset clientset.Interface,
	fhrInformerFactory ifinformers.SharedInformerFactory,
	release *chartrelease.Release) *Controller {

	// Obtain reference to shared index informers for the FluxHelmResource
	fhrInformer := fhrInformerFactory.Integrations().V1().FluxHelmResources()

	// Add helm-operator types to the default Kubernetes Scheme so Events can be
	// logged for helm-operator types.
	ifscheme.AddToScheme(scheme.Scheme)
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		logger: logger,
		// kubeclientset:    kubeclientset,
		// fhrclientset:     fhrclientset,
		fhrLister:        fhrInformer.Lister(),
		fhrSynced:        fhrInformer.Informer().HasSynced,
		release:          release,
		releaseWorkqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ChartRelease"),
		recorder:         recorder,
	}

	fmt.Println("===> Setting up event handlers")
	controller.logger.Log("info", "Setting up event handlers")
	fmt.Println("===> still setting up event handlers")

	// --------------------------------------------------------------------
	// ----- EVENT HANDLERS for FluxHelmResource resources change ---------
	fhrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(new interface{}) {
			fmt.Println("\n\t>>> ADDING release\n")
			controller.enqueueJob(new)
		},
		UpdateFunc: func(old, new interface{}) {
			fmt.Printf("\n>>> in UpdateFunc\n")

			oldMeta, err := meta.Accessor(old)
			if err != nil {
				controller.logger.Log("error", fmt.Sprintf("%#v", err))
				controller.discardJob()
				return
			}
			newMeta, err := meta.Accessor(new)
			if err != nil {
				controller.logger.Log("error", fmt.Sprintf("%#v", err))
				controller.discardJob()
				return
			}

			oldResVersion := oldMeta.GetResourceVersion()
			newResVersion := newMeta.GetResourceVersion()
			fmt.Printf("*** old META resource version ... %#v\n", oldResVersion)
			fmt.Printf("*** new META resource version ... %#v\n", newResVersion)

			if newResVersion != oldResVersion {
				fmt.Println("\n\t>>> UPDATING release\n")
				controller.enqueueJob(new)
			}
		},
		DeleteFunc: func(old interface{}) {
			fhr, ok := checkCustomResourceType(old)
			if !ok {
				controller.logger.Log("error", fmt.Sprintf("FluxHelmResource Event Watch received an invalid object: %#v", old))
				return
			}
			fmt.Printf("\n\t>>> DELETING release\n")
			name := chartrelease.GetReleaseName(fhr)
			err := controller.deleteRelease(name)
			if err != nil {
				controller.logger.Log("error", fmt.Sprintf("Chart release [%s] not deleted: %#v", name, err))
			}
		},
	})
	fmt.Println("===> event handlers are set up")

	return controller
}

// Run sets up the event handlers for our Custom Resource, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.releaseWorkqueue.ShutDown()

	c.logger.Log("info", ">>> Starting operator <<<")
	// Wait for the caches to be synced before starting workers
	c.logger.Log("info", "----------> Waiting for informer caches to sync")

	if ok := cache.WaitForCacheSync(stopCh, c.fhrSynced); !ok {
		return fmt.Errorf("error: %s", "failed to wait for caches to sync")
	}
	c.logger.Log("info", "<---------- informer caches synced")

	c.logger.Log("info", "=== Starting workers ===")
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
	c.logger.Log("info", "=== Stopping workers ===")

	return nil
}

// runWorker is a long-running function calling the
// processNextWorkItem function to read and process a message
// on a workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// discardJob unconditionally removes an item from the workqueue
// this should never happen, but is a safety catch for a situation
// the workqueue receives an incorrect item
func (c *Controller) discardJob() {
	obj, shutdown := c.releaseWorkqueue.Get()
	c.logger.Log("info", fmt.Sprintf("\t\t\t---> discarding item\n\n[%#v]\n\n", obj))
	if shutdown {
		return
	}
	c.releaseWorkqueue.Forget(obj)
	c.releaseWorkqueue.Done(obj)
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	c.logger.Log("info", "*** in processNextWorkItem")

	obj, shutdown := c.releaseWorkqueue.Get()
	c.logger.Log("info", fmt.Sprintf("---> PROCESSING item [%#v]", obj))

	if shutdown {
		return false
	}

	// wrapping block in a func to defer c.workqueue.Done
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We must call Forget if we
		// do not want this work item being re-queued. If a transient error occurs, , we do
		// not call Forget. Instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.releaseWorkqueue.Done(obj)

		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form "namespace/fhr(custom resource) name". We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date than when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget not to get into a loop of attempting to
			// process a work item that is invalid.
			c.releaseWorkqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))

			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// FluxHelmResource resource to sync the corresponding Chart release.
		// If the sync failed, then we return while the item will get requeued
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}
		// If no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.releaseWorkqueue.Forget(obj)

		c.logger.Log("info", fmt.Sprintf("\t\t *** Successfully synced '%s'", key))
		c.logger.Log("info", fmt.Sprintf("\n---> WORK QUEUE length is %d now\n\n", c.releaseWorkqueue.Len()))

		return nil
	}(obj)

	if err != nil {
		c.logger.Log("info", fmt.Sprintf("Caught runtime error: %#v", err))
		runtime.HandleError(err)
		return true
	}
	return true
}

// syncHandler acts according to the action
// 		Deletes/creates or updates a Chart release
//------------------------------------------------------------------------
func (c *Controller) syncHandler(key string) error {
	c.logger.Log("info", "*** in syncHandler")

	// Retrieve namespace and Custom Resource name from the key
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.logger.Log("info", fmt.Sprintf("Invalid cache key: %v", err))
		runtime.HandleError(fmt.Errorf("Invalid cache key: %s", key))
		return nil
	}

	// Custom Resource fhr contains all information we need to know about the Chart release
	fhr, err := c.fhrLister.FluxHelmResources(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			c.logger.Log("info", fmt.Sprintf("Fluxhelmresource '%s' referred to in work queue no longer exists", key))
			runtime.HandleError(fmt.Errorf("Fluxhelmresource '%s' referred to in work queue no longer exists", key))
			return nil
		}
		c.logger.Log("error", err.Error())
		return err
	}

	releaseName := chartrelease.GetReleaseName(*fhr)
	// find if release exists
	_, err = c.release.Get(releaseName)
	//c.logger.Log("info", fmt.Sprintf("+++++ Getting release: rls = %#v", rls))
	//c.logger.Log("info", fmt.Sprintf("+++++ Getting release: error = %#v", err))
	c.logger.Log("info", fmt.Sprintf("Error when getting release: err.Error() = %s", err.Error()))

	var syncType chartrelease.ReleaseType
	if err != nil {
		if err.Error() == "NOT EXISTS" {
			syncType = chartrelease.ReleaseType("CREATE")
			c.logger.Log("info", fmt.Sprintf("Creating a new Chart release: %s", releaseName))
		} else {
			c.logger.Log("error", fmt.Sprintf("Failure to do Chart release [%s]: %#v", releaseName, err))
			return err
		}
	}
	if err == nil {
		syncType = chartrelease.ReleaseType("UPDATE")
	}
	_, err = c.release.Install(releaseName, *fhr, syncType)
	if err != nil {
		return err
	}

	c.recorder.Event(fhr, corev1.EventTypeNormal, ChartSynced, MessageChartSynced)
	return nil
}

func checkCustomResourceType(obj interface{}) (ifv1.FluxHelmResource, bool) {
	_, err := meta.Accessor(obj)
	if err != nil {
		return ifv1.FluxHelmResource{}, false
	}

	var fhr *ifv1.FluxHelmResource
	var ok bool
	if fhr, ok = obj.(*ifv1.FluxHelmResource); !ok {
		return ifv1.FluxHelmResource{}, false
	}

	return *fhr, true
}

func getCacheKey(obj interface{}) (string, error) {
	var key string
	var err error

	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		fmt.Printf("*** caught runtime ERROR: %#v\n", err)
		runtime.HandleError(err)
		return "", err
	}
	return key, nil
}

// enqueueJob takes a FluxHelmResource resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should not be
// passed resources of any type other than FluxHelmResource.
func (c *Controller) enqueueJob(obj interface{}) {
	fmt.Println("=== in enqueueJob")

	var key string
	var err error
	if key, err = getCacheKey(obj); err != nil {
		return
	}

	c.releaseWorkqueue.AddRateLimited(key)
	c.logger.Log("info", fmt.Sprintf("\n\t\t===> appended key %s ... current WORK QUEUE length is %d <===\n\n", key, c.releaseWorkqueue.Len()))
}

func (c *Controller) deleteRelease(name string) error {
	err := c.release.Delete(name)
	if err != nil {
		return err
	}
	return nil
}
