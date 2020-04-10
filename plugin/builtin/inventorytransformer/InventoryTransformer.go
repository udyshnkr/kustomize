// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

//go:generate pluginator
package main

import (
	"fmt"

	"sigs.k8s.io/kustomize/api/hasher"
	"sigs.k8s.io/kustomize/api/inventory"
	"sigs.k8s.io/kustomize/api/kv"
	"sigs.k8s.io/kustomize/api/resid"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/yaml"
)

type plugin struct {
	h                *resmap.PluginHelpers
	types.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Policy           string `json:"policy,omitempty" yaml:"policy,omitempty"`
}

//noinspection GoUnusedGlobalVariable
var KustomizePlugin plugin

func (p *plugin) Config(
	h *resmap.PluginHelpers, c []byte) (err error) {
	p.h = h
	err = yaml.Unmarshal(c, p)
	if err != nil {
		return err
	}
	if p.Policy == "" {
		p.Policy = types.GarbageIgnore.String()
	}
	if p.Policy != types.GarbageCollect.String() &&
		p.Policy != types.GarbageIgnore.String() {
		return fmt.Errorf(
			"unrecognized garbagePolicy '%s'", p.Policy)
	}
	return nil
}

// Transform generates an inventory object from the input ResMap.
// This ConfigMap supports the pruning command in
// the client side tool proposed here:
// https://github.com/kubernetes/enhancements/pull/810
//
// The inventory data is written to the ConfigMap's
// annotations, rather than to the key-value pairs in
// the ConfigMap's data field, since
//   1. Keys in a ConfigMap's data field are too
//      constrained for this purpose.
//   2. Using annotations allow any object to be used,
//      not just a ConfigMap, should some other object
//      (e.g. some App object) become more desirable
//      for this purpose.
func (p *plugin) Transform(m resmap.ResMap) error {
	inv, h, err := makeInventory(m)
	if err != nil {
		return err
	}
	args := types.ConfigMapArgs{}
	args.Name = p.Name
	args.Namespace = p.Namespace
	args.Options = &types.GeneratorOptions{
		Annotations: map[string]string{inventory.HashAnnotation: h},
	}
	err = inv.UpdateAnnotations(args.Options.Annotations)
	if err != nil {
		return err
	}

	cm, err := p.h.ResmapFactory().RF().MakeConfigMap(
		kv.NewLoader(p.h.Loader(), p.h.Validator()), &args)
	if err != nil {
		return err
	}

	if p.Policy == types.GarbageCollect.String() {
		for _, byeBye := range m.AllIds() {
			m.Remove(byeBye)
		}
	}
	return m.Append(cm)
}

func makeInventory(m resmap.ResMap) (
	inv *inventory.Inventory, hash string, err error) {
	inv = inventory.NewInventory()
	var keys []string
	for _, r := range m.Resources() {
		ns := r.GetNamespace()
		item := resid.NewResIdWithNamespace(r.GetGvk(), r.GetName(), ns)
		if _, ok := inv.Current[item]; ok {
			return nil, "", fmt.Errorf(
				"item '%v' already in inventory", item)
		}
		inv.Current[item], err = computeRefs(r, m)
		if err != nil {
			return nil, "", err
		}
		keys = append(keys, item.String())
	}
	h, err := hasher.SortArrayAndComputeHash(keys)
	return inv, h, err
}

func computeRefs(
	r *resource.Resource, m resmap.ResMap) (refs []resid.ResId, err error) {
	for _, refid := range r.GetRefBy() {
		ref, err := m.GetByCurrentId(refid)
		if err != nil {
			return nil, err
		}
		refs = append(
			refs,
			resid.NewResIdWithNamespace(
				ref.GetGvk(), ref.GetName(), ref.GetNamespace()))
	}
	return
}
