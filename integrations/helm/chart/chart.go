package chart

import (
	"fmt"

	"k8s.io/helm/pkg/helm"
	helm_env "k8s.io/helm/pkg/helm/environment"
	rls "k8s.io/helm/pkg/proto/hapi/services"
)

var (
	hc       helm.Client
	options  []helm.Option
	voption  helm.VersionOption
	settings helm_env.EnvSettings
)

func init() {
	//	settings.TillerHost = ":44134"
	settings.TillerHost = "10.96.39.164:44134"
	options = []helm.Option{helm.Host(settings.TillerHost)}
	voption = helm.VersionOption(helm.Host(settings.TillerHost))

}

// Helm client
//		setup configuration
//		create client
// Charts
//		IsChartReleased
//		DoRelease
//		DeleteRelease
//

func GetChart() {
	hcl := helm.NewClient(helm.Host(settings.TillerHost))

	fmt.Printf("hcl: %#v\n", hcl)

	var v *rls.GetVersionResponse
	var err error
	if v, err = hcl.GetVersion(voption); err == nil {
		fmt.Printf("Error: %#v\n", err)
	}
	fmt.Printf("Tiller version is: %#v\n", v)

	response, _ := hcl.ListReleases()

	fmt.Printf("Number of helm releases is %d\n", response.GetCount())

	for i, r := range response.GetReleases() {
		fmt.Printf("\t--> %d : %#v\n\n%#v\n\n%#v\n\n%#v\n\n", i, r.Name, r.Namespace, r.Chart, r.Config)
	}
}
