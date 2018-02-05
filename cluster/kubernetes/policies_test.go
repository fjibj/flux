package kubernetes

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/weaveworks/flux"
	"github.com/weaveworks/flux/policy"
)

var changes = []struct {
	name    string
	in, out map[string]string
	update  policy.Update
}{
	{
		name: "adding annotation with others existing",
		in:   map[string]string{"prometheus.io.scrape": "false"},
		out:  map[string]string{"flux.weave.works/automated": "true", "prometheus.io.scrape": "false"},
		update: policy.Update{
			Add: policy.Set{policy.Automated: "true"},
		},
	},
	{
		name: "adding annotation when already has annotation",
		in:   map[string]string{"flux.weave.works/automated": "true"},
		out:  map[string]string{"flux.weave.works/automated": "true"},
		update: policy.Update{
			Add: policy.Set{policy.Automated: "true"},
		},
	},
	{
		name: "adding annotation when already has annotation and others",
		in:   map[string]string{"flux.weave.works/automated": "true", "prometheus.io.scrape": "false"},
		out:  map[string]string{"flux.weave.works/automated": "true", "prometheus.io.scrape": "false"},
		update: policy.Update{
			Add: policy.Set{policy.Automated: "true"},
		},
	},
	{
		name: "adding first annotation",
		in:   nil,
		out:  map[string]string{"flux.weave.works/automated": "true"},
		update: policy.Update{
			Add: policy.Set{policy.Automated: "true"},
		},
	},
	{
		name: "add and remove different annotations at the same time",
		in:   map[string]string{"flux.weave.works/automated": "true", "prometheus.io.scrape": "false"},
		out:  map[string]string{"flux.weave.works/locked": "true", "prometheus.io.scrape": "false"},
		update: policy.Update{
			Add:    policy.Set{policy.Locked: "true"},
			Remove: policy.Set{policy.Automated: "true"},
		},
	},
	{
		name: "remove overrides add for same key",
		in:   nil,
		out:  nil,
		update: policy.Update{
			Add:    policy.Set{policy.Locked: "true"},
			Remove: policy.Set{policy.Locked: "true"},
		},
	},
	{
		name: "remove annotation with others existing",
		in:   map[string]string{"flux.weave.works/automated": "true", "prometheus.io.scrape": "false"},
		out:  map[string]string{"prometheus.io.scrape": "false"},
		update: policy.Update{
			Remove: policy.Set{policy.Automated: "true"},
		},
	},
	{
		name: "remove last annotation",
		in:   map[string]string{"flux.weave.works/automated": "true"},
		out:  nil,
		update: policy.Update{
			Remove: policy.Set{policy.Automated: "true"},
		},
	},
	{
		name: "remove annotation with no annotations",
		in:   nil,
		out:  nil,
		update: policy.Update{
			Remove: policy.Set{policy.Automated: "true"},
		},
	},
	{
		name: "remove annotation with only others",
		in:   map[string]string{"prometheus.io.scrape": "false"},
		out:  map[string]string{"prometheus.io.scrape": "false"},
		update: policy.Update{
			Remove: policy.Set{policy.Automated: "true"},
		},
	},
}

func TestUpdatePolicies(t *testing.T) {
	for _, c := range changes {
		id := flux.MakeResourceID("default", "deployment", "nginx")
		caseIn := templToString(t, annotationsTemplate, c.in)
		caseOut := templToString(t, annotationsTemplate, c.out)
		out, err := (&Manifests{}).UpdatePolicies([]byte(caseIn), id, c.update)

		if err != nil {
			t.Errorf("[%s] %v", c.name, err)
		} else if string(out) != caseOut {
			t.Errorf("[%s] Did not get expected result:\n\n%s\n\nInstead got:\n\n%s", c.name, caseOut, string(out))
		}

	}
}

func TestUpdateListPolicies(t *testing.T) {
	for _, c := range changes {
		id := flux.MakeResourceID("default", "deployment", "a-deployment")
		listIn := templToString(t, listAnnotationsTemplate, c.in)
		listOut := templToString(t, listAnnotationsTemplate, c.out)
		out, err := (&Manifests{}).UpdatePolicies([]byte(listIn), id, c.update)

		if err != nil {
			t.Fatalf("[%s] %v", c.name, err)
		}

		if string(out) != listOut {
			t.Errorf("have: %v\nwant: %v\n", string(out), listOut)
		}
	}
}

var annotationsTemplate = template.Must(template.New("").Parse(`---
apiVersion: extensions/v1beta1
kind: Deployment
metadata: # comment really close to the war zone
  {{with .}}annotations:{{range $k, $v := .}}
    {{$k}}: {{printf "%q" $v}}{{end}}
  {{end}}name: nginx
spec:
  replicas: 1
  template:
    metadata: # comment2
      labels:
        name: nginx
    spec:
      containers:
      - image: nginx  # These keys are purposefully un-sorted.
        name: nginx   # And these comments are testing comments.
        ports:
        - containerPort: 80
`))

var listAnnotationsTemplate = template.Must(template.New("").Parse(`---
apiVersion: v1
kind: List
items:
  - apiVersion: extensions/v1beta1
    kind: Deployment
    metadata:
    {{with .}}  annotations:{{range $k, $v := .}}
        {{$k}}: {{printf "%q" $v}}{{end}}
    {{end}}  name: a-deployment
    spec:
      template:
        metadata:
          labels:
            name: a-deployment
        spec:
          containers:
          - name: a-container
            image: quay.io/weaveworks/helloworld:master-a000001
  - apiVersion: extensions/v1beta1
    kind: Deployment
    metadata:
    {{with .}}  annotations:{{range $k, $v := .}}
        {{$k}}: {{printf "%q" $v}}{{end}}
    {{end}}  name: b-deployment
    spec:
      template:
        metadata:
          labels:
            name: b-deployment
        spec:
          containers:
          - name: b-container
            image: quay.io/weaveworks/helloworld:master-a000001
`))

func templToString(t *testing.T, templ *template.Template, data interface{}) string {
	out := &bytes.Buffer{}
	err := templ.Execute(out, data)
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}
