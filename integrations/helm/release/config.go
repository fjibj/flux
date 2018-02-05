package release

import (
	ifv1 "github.com/weaveworks/flux/apis/integrations.flux/v1"
	ifclientset "github.com/weaveworks/flux/integrations/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func GetAllCustomResources(ifClient *ifclientset.Clientset) ([]ifv1.FluxHelmResource, error) {
	list, err := ifClient.IntegrationsV1().FluxHelmResources("").List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// GetAllChartCustomResources retrieves custom resources for one particular Chart
// specified by its git repo path (with any slash replaced by _)
func GetAllChartCustomResources(ifClient *ifclientset.Clientset, chartLabel string) ([]ifv1.FluxHelmResource, error) {
	if chartLabel == "" {
		return GetAllCustomResources(ifClient)
	}

	chartSelector := map[string]string{
		"chart": chartLabel,
	}
	labelsSet := labels.Set(chartSelector)
	listOptions := metav1.ListOptions{LabelSelector: labelsSet.AsSelector().String()}
	list, err := ifClient.IntegrationsV1().FluxHelmResources("").List(listOptions)
	if err != nil {
		return nil, err
	}

	return list.Items, nil
}

// GetNSChartCustomResources retrieves custom resources for one particular Chart in a particular namespace
// specified by its git repo path (with any slash replaced by _)
func GetNSChartCustomResources(ifClient *ifclientset.Clientset, ns string, chartLabel string) ([]ifv1.FluxHelmResource, error) {
	listOptions := &metav1.ListOptions{}

	if chartLabel != "" {
		chartSelector := map[string]string{
			"chart": chartLabel,
		}
		labelSet := labels.Set(chartSelector)
		listOptions.LabelSelector = labelSet.AsSelector().String()
	}
	list, err := ifClient.IntegrationsV1().FluxHelmResources(ns).List(*listOptions)
	if err != nil {
		return nil, err
	}

	return list.Items, nil
}
