package operator

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

	fhrSynced    cache.InformerSynced
	chartsSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewController returns a new helm-operator
func NewController(
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

		// TODO implement chartInformer to have chartInformer.Informer().HasSynced
		chartsSynced: fhrInformer.Informer().HasSynced,
		workqueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "FluxHelmResources"),
		recorder:     recorder,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when FluxHelmResource resources change
	fhrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueFluxHelmResource,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueFluxHelmResource(new)
		},
	})
	// Set up an event handler for when Deployment resources change. This
	// handler will lookup the owner of the given Deployment, and if it is
	// owned by a FluxHelmResource resource will enqueue that FluxHelmResource resource for
	// processing. This way, we don't need to implement custom logic for
	// handling Deployment resources. More info on this pattern:
	// https://github.com/kubernetes/community/blob/8cafef897a22026d42f5e5bb3f104febe7e29830/contributors/devel/controllers.md
	/*
		deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: controller.handleObject,
			UpdateFunc: func(old, new interface{}) {
				newDepl := new.(*appsv1beta2.Deployment)
				oldDepl := old.(*appsv1beta2.Deployment)
				if newDepl.ResourceVersion == oldDepl.ResourceVersion {
					// Periodic resync will send update events for all known Deployments.
					// Two different versions of the same Deployment will always have different RVs.
					return
				}
				controller.handleObject(new)
			},
			DeleteFunc: controller.handleObject,
		})
	*/

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	glog.Info("********************************")
	glog.Info("*** Starting helm-controller ***")
	glog.Info("********************************")

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting helm-controller")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.chartsSynced, c.fhrSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch two workers to process FluxHelmResource resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// FluxHelmResource resource to be synced.
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
//-------------------------------------------------------
// converge the two. It then updates the Status block of the FluxHelmResource resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the FluxHelmResource resource with this namespace/name
	fhr, err := c.fhrLister.FluxHelmResources(namespace).Get(name)
	if err != nil {
		// The FluxHelmResource resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("fluxhelmresource '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	spec := fhr.Spec

	fmt.Printf("=== %#v\n\n", spec)

	// Sanity check of input - if needed
	if spec.Chart == "" {
		// We choose to absorb the error here as the worker would requeue the
		// resource otherwise. Instead, the next time the resource is updated
		// the resource will be queued again.
		runtime.HandleError(fmt.Errorf("%s: chart name must be specified", key))

		// TODO a message should be sent to inform about the malconfigured custom resource
		return nil
	}

	/*
		deploymentName := fhr.Spec.Customizations
		if deploymentName == "" {
			// We choose to absorb the error here as the worker would requeue the
			// resource otherwise. Instead, the next time the resource is updated
			// the resource will be queued again.
			runtime.HandleError(fmt.Errorf("%s: deployment name must be specified", key))
			return nil
		}

		// Get the deployment with the name specified in FluxHelmResource.spec
		deployment, err := c.deploymentsLister.Deployments(fhr.Namespace).Get(deploymentName)
		// If the resource doesn't exist, we'll create it
		if errors.IsNotFound(err) {
			deployment, err = c.kubeclientset.AppsV1beta2().Deployments(fhr.Namespace).Create(newDeployment(fhr))
		}

		// If an error occurs during Get/Create, we'll requeue the item so we can
		// attempt processing again later. This could have been caused by a
		// temporary network failure, or any other transient reason.
		if err != nil {
			return err
		}

		// If the Deployment is not controlled by this FluxHelmResource resource, we should log
		// a warning to the event recorder and ret
		if !metav1.IsControlledBy(deployment, fhr) {
			msg := fmt.Sprintf(MessageResourceExists, deployment.Name)
			c.recorder.Event(fhr, corev1.EventTypeWarning, ErrResourceExists, msg)
			return fmt.Errorf(msg)
		}

		// If this number of the replicas on the FluxHelmResource resource is specified, and the
		// number does not equal the current desired replicas on the Deployment, we
		// should update the Deployment resource.
		if fhr.Spec.Replicas != nil && *fhr.Spec.Replicas != *deployment.Spec.Replicas {
			glog.V(4).Infof("FluxHelmResourcer: %d, deplR: %d", *fhr.Spec.Replicas, *deployment.Spec.Replicas)
			deployment, err = c.kubeclientset.AppsV1beta2().Deployments(fhr.Namespace).Update(newDeployment(fhr))
		}

		// If an error occurs during Update, we'll requeue the item so we can
		// attempt processing again later. THis could have been caused by a
		// temporary network failure, or any other transient reason.
		if err != nil {
			return err
		}

		// Finally, we update the status block of the FluxHelmResource resource to reflect the
		// current state of the world
		err = c.updateFluxHelmResourceStatus(fhr, deployment)
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

// enqueueFluxHelmResource takes a FluxHelmResource resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than FluxHelmResource.
func (c *Controller) enqueueFluxHelmResource(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}

// handleObject will take any resource implementing metav1.Object and attempt
// to find the FluxHelmResource resource that 'owns' it. It does this by looking at the
// objects metadata.ownerReferences field for an appropriate OwnerReference.
// It then enqueues that FluxHelmResource resource to be processed. If the object does not
// have an appropriate OwnerReference, it will simply be skipped.
func (c *Controller) handleObject(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		glog.V(4).Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}
	glog.V(4).Infof("Processing object: %s", object.GetName())
	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		// If this object is not owned by a FluxHelmResource, we should not do anything more
		// with it.
		if ownerRef.Kind != "FluxHelmResource" {
			return
		}

		fhr, err := c.fhrLister.FluxHelmResources(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			glog.V(4).Infof("ignoring orphaned object '%s' of fhr '%s'", object.GetSelfLink(), ownerRef.Name)
			return
		}

		c.enqueueFluxHelmResource(fhr)
		return
	}
}

// newDeployment creates a new Deployment for a FluxHelmResource resource. It also sets
// the appropriate OwnerReferences on the resource so handleObject can discover
// the FluxHelmResource resource that 'owns' it.
func newDeployment(fhr *ifv1.FluxHelmResource) *appsv1beta2.Deployment {
	labels := map[string]string{
		"app":        "nginx",
		"controller": fhr.Name,
	}
	return &appsv1beta2.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			//Name:      fhr.Spec.DeploymentName,
			Name:      fhr.Name,
			Namespace: fhr.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(fhr, schema.GroupVersionKind{
					Group:   ifv1.SchemeGroupVersion.Group,
					Version: ifv1.SchemeGroupVersion.Version,
					Kind:    "FluxHelmResource",
				}),
			},
		},
		Spec: appsv1beta2.DeploymentSpec{
			//Replicas: fhr.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}
}

//func newChartRelease(fhr *ifv1.FluxHelmResource) *appsv1beta2.Deployment {
func newChartRelease(fhr *ifv1.FluxHelmResource) {

	fmt.Printf(">>> Released new chart for %#v\n\n", fhr)
	/*
		labels := map[string]string{
			"app":        "nginx",
			"controller": fhr.Name,
		}
		return &appsv1beta2.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				//Name:      fhr.Spec.DeploymentName,
				Name:      fhr.Name,
				Namespace: fhr.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(fhr, schema.GroupVersionKind{
						Group:   ifv1.SchemeGroupVersion.Group,
						Version: ifv1.SchemeGroupVersion.Version,
						Kind:    "FluxHelmResource",
					}),
				},
			},
			Spec: appsv1beta2.DeploymentSpec{
				//Replicas: fhr.Spec.Replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx:latest",
							},
						},
					},
				},
			},
		}
	*/
}

func chartReleaseUpdate(fhr *ifv1.FluxHelmResource) {

	fmt.Printf(">>> Updated release for new chart for %#v\n\n", fhr)
}
