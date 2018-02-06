package kubernetes

import (
	"io/ioutil"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"

	"github.com/weaveworks/flux"
	"github.com/weaveworks/flux/cluster/kubernetes/resource"
	"github.com/weaveworks/flux/policy"
)

func (m *Manifests) UpdatePolicies(in []byte, serviceID flux.ResourceID, update policy.Update) ([]byte, error) {
	tagAll, _ := update.Add.Get(policy.TagAll)

	var b []byte

	u, err := updateAnnotations(in, serviceID, tagAll, func(a map[string]string) map[string]string {
		for p, v := range update.Add {
			if p == policy.TagAll {
				continue
			}
			a[resource.PolicyPrefix+string(p)] = v
		}
		for p, _ := range update.Remove {
			delete(a, resource.PolicyPrefix+string(p))
		}
		return a
	})

	if err != nil {
		return nil, err
	}

	b = append(b, u...)

	return b, nil
}

func updateAnnotations(def []byte, serviceID flux.ResourceID, tagAll string, f func(map[string]string) map[string]string) ([]byte, error) {
	manifest, err := parseManifest(def)
	if err != nil {
		return nil, err
	}

	str := string(def)
	isList := strings.Contains(str, "kind: List")
	var annotations map[string]string
	var containers []resource.Container
	var annotationsExpression string
	var metadataExpression string
	if isList {
		var l resource.List
		err := yaml.Unmarshal(def, &l)
		// find the item we are trying to update in the List
		for _, item := range l.Items {
			if item.ResourceID().String() == serviceID.String() {
				annotations = item.Metadata.AnnotationsOrNil()
				containers = item.Spec.Template.Spec.Containers
				break
			}
		}
		if err != nil {
			return nil, err
		}

		annotationsExpression = `(?m:\n\s{6}annotations:\s*(?:#.*)*(?:\n\s{8}.*)*$)`
		metadataExpression = `(?m:\n\s{4}metadata:\s*(?:#.*)*(?:\n\s*.*))`
	} else {
		annotationsExpression = `(?m:\n  annotations:\s*(?:#.*)*(?:\n    .*)*$)`
		metadataExpression = `(?m:^(metadata:\s*(?:#.*)*)$)`
		annotations = manifest.Metadata.AnnotationsOrNil()
		containers = manifest.Spec.Template.Spec.Containers
	}

	if tagAll != "" {
		for _, c := range containers {
			p := resource.PolicyPrefix + string(policy.TagPrefix(c.Name))
			if tagAll != "glob:*" {
				annotations[p] = tagAll
			} else {
				delete(annotations, p)
			}
		}
	}
	newAnnotations := f(annotations)

	// Write the new annotations back into the manifest
	// Generate a fragment of the new annotations.
	var fragment string
	if len(newAnnotations) > 0 {
		fragmentB, err := yaml.Marshal(map[string]map[string]string{
			"annotations": newAnnotations,
		})
		if err != nil {
			return nil, err
		}

		fragment = string(fragmentB)

		// Remove the last newline, so it fits in better
		fragment = strings.TrimSuffix(fragment, "\n")

		// indent the fragment 2 spaces
		fragment = regexp.MustCompile(`(.+)`).ReplaceAllString(fragment, "  $1")

		// Add a newline if it's not blank
		if len(fragment) > 0 {
			fragment = "\n" + fragment
		}
	}

	if isList {
		fragment = regexp.MustCompile(`(.+)`).ReplaceAllString(fragment, "  $1")
		fragment = regexp.MustCompile(`(.+)`).ReplaceAllString(fragment, "  $1")
	}

	// Find where to insert the fragment.
	// TODO: This should handle potentially different indentation.
	// TODO: There's probably a more elegant regex-ey way to do this in one pass.
	replaced := false
	annotationsRE := regexp.MustCompile(annotationsExpression)
	newDef := annotationsRE.ReplaceAllStringFunc(str, func(found string) string {
		if !replaced {
			replaced = true
			return fragment
		}
		return found
	})
	if !replaced {
		metadataRE := regexp.MustCompile(metadataExpression)
		newDef = metadataRE.ReplaceAllStringFunc(str, func(found string) string {
			if !replaced {
				replaced = true
				f := found + fragment
				return f
			}
			return found
		})
	}
	if !replaced {
		return nil, errors.New("Could not update resource annotations")
	}

	return []byte(newDef), err
}

func parseManifest(def []byte) (resource.BaseObject, error) {
	var m resource.BaseObject
	if err := yaml.Unmarshal(def, &m); err != nil {
		return m, errors.Wrap(err, "decoding annotations")
	}
	return m, nil
}

func (m *Manifests) ServicesWithPolicies(root string) (policy.ResourceMap, error) {
	all, err := m.FindDefinedServices(root)
	if err != nil {
		return nil, err
	}

	result := map[flux.ResourceID]policy.Set{}
	err = iterateManifests(all, func(s flux.ResourceID, m resource.BaseObject) error {
		ps, err := policiesFrom(m)
		if err != nil {
			return err
		}
		result[s] = ps
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func iterateManifests(services map[flux.ResourceID][]string, f func(flux.ResourceID, resource.BaseObject) error) error {
	for serviceID, paths := range services {
		if len(paths) != 1 {
			continue
		}

		def, err := ioutil.ReadFile(paths[0])
		if err != nil {
			return err
		}
		manifest, err := parseManifest(def)
		if err != nil {
			return err
		}

		if err = f(serviceID, manifest); err != nil {
			return err
		}
	}
	return nil
}

func policiesFrom(m resource.BaseObject) (policy.Set, error) {
	var policies policy.Set
	for k, v := range m.Metadata.AnnotationsOrNil() {
		if !strings.HasPrefix(k, resource.PolicyPrefix) {
			continue
		}
		p := policy.Policy(strings.TrimPrefix(k, resource.PolicyPrefix))
		if policy.Boolean(p) {
			if v != "true" {
				continue
			}
			policies = policies.Add(p)
		} else {
			policies = policies.Set(p, v)
		}
	}
	return policies, nil
}
