/*
Copyright 2024 NVIDIA CORPORATION & AFFILIATES

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package crdutil

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CRDOperation defines the type of operation to perform on CRDs
type CRDOperation string

const (
	// CRDOperationApply creates or updates CRDs
	CRDOperationApply CRDOperation = "apply"
	// CRDOperationDelete deletes CRDs
	CRDOperationDelete CRDOperation = "delete"
)

// ProcessCRDs processes CRDs from the given paths based on the operation type.
// It accepts both directories (walked recursively) and individual YAML files.
// For each CRD found, it performs the specified operation (apply or delete).
func ProcessCRDs(ctx context.Context, operation CRDOperation, crdPaths ...string) error {
	if len(crdPaths) == 0 {
		return errors.New("at least one CRD path (file or directory) is required")
	}

	config, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	return ProcessCRDsWithConfig(ctx, config, operation, crdPaths...)
}

// ProcessCRDsWithConfig processes CRDs using a provided Kubernetes REST config.
// It accepts both directories (walked recursively) and individual YAML files,
// parses them, and either applies or deletes them from the cluster.
func ProcessCRDsWithConfig(ctx context.Context, config *rest.Config, operation CRDOperation, crdPaths ...string) error {
	client, err := clientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create API extensions client: %w", err)
	}

	crdFilePaths, err := walkCRDPaths(crdPaths)
	if err != nil {
		return fmt.Errorf("failed to walk CRD paths: %w", err)
	}

	if len(crdFilePaths) == 0 {
		log.Printf("No CRD files found in paths: %v", crdPaths)
		return nil
	}

	crds, err := parseCRDsFromPaths(crdFilePaths)
	if err != nil {
		return fmt.Errorf("failed to parse CRDs: %w", err)
	}

	if len(crds) == 0 {
		log.Printf("No valid CRDs found in %d file(s)", len(crdFilePaths))
		return nil
	}

	switch operation {
	case CRDOperationApply:
		log.Printf("Applying %d CRD(s) from %d file(s)", len(crds), len(crdFilePaths))
		if err := applyCRDs(ctx, client, crds); err != nil {
			return fmt.Errorf("failed to apply CRDs: %w", err)
		}
		if err := waitForCRDs(ctx, config, crds); err != nil {
			return fmt.Errorf("failed waiting for CRDs to be established: %w", err)
		}
		log.Printf("Successfully applied %d CRD(s)", len(crds))
		return nil

	case CRDOperationDelete:
		log.Printf("Deleting %d CRD(s) from %d file(s)", len(crds), len(crdFilePaths))
		if err := deleteCRDs(ctx, client, crds); err != nil {
			return fmt.Errorf("failed to delete CRDs: %w", err)
		}
		log.Printf("Successfully processed %d CRD deletion(s)", len(crds))
		return nil

	default:
		return fmt.Errorf("unknown operation: %s", operation)
	}
}

// walkCRDPaths recursively walks the given paths (files or directories) and returns
// a list of all YAML/YML files found. If a path is a file, it's included directly.
// If a path is a directory, it's walked recursively to find all YAML/YML files.
func walkCRDPaths(paths []string) ([]string, error) {
	var crdPaths []string
	validExts := map[string]bool{
		".yaml": true,
		".yml":  true,
	}

	for _, p := range paths {
		err := filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Skip directories
			if d.IsDir() {
				return nil
			}
			// Only include YAML/YML files
			if validExts[filepath.Ext(path)] {
				crdPaths = append(crdPaths, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to walk path %s: %w", p, err)
		}
	}

	return crdPaths, nil
}

// parseCRDsFromPaths reads and parses CRD YAML files from the given paths.
func parseCRDsFromPaths(paths []string) ([]*apiextensionsv1.CustomResourceDefinition, error) {
	var crds []*apiextensionsv1.CustomResourceDefinition

	for _, path := range paths {
		fileCRDs, err := parseCRDsFromFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CRDs from %s: %w", path, err)
		}
		crds = append(crds, fileCRDs...)
	}

	return crds, nil
}

// parseCRDsFromFile reads a YAML file and parses all CRD documents from it.
func parseCRDsFromFile(filePath string) ([]*apiextensionsv1.CustomResourceDefinition, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var crds []*apiextensionsv1.CustomResourceDefinition
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	for {
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read YAML document: %w", err)
		}

		// Skip empty documents
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(doc, crd); err != nil {
			// Skip documents that aren't valid CRDs
			log.Printf("warning: skipping invalid CRD document: %v", err)
			continue
		}

		if crd.Kind != "CustomResourceDefinition" || crd.Spec.Names.Kind == "" || crd.Spec.Group == "" {
			continue
		}

		crds = append(crds, crd)
	}

	return crds, nil
}

// applyCRDs creates or updates CRDs in the cluster.
func applyCRDs(
	ctx context.Context,
	client *clientset.Clientset,
	crds []*apiextensionsv1.CustomResourceDefinition,
) error {
	crdClient := client.ApiextensionsV1().CustomResourceDefinitions()

	for _, crd := range crds {
		_, err := crdClient.Get(ctx, crd.Name, metav1.GetOptions{})

		switch {
		case apierrors.IsNotFound(err):
			log.Printf("Creating CRD: %s", crd.Name)
			if _, err := crdClient.Create(ctx, crd, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create CRD %s: %w", crd.Name, err)
			}
		case err != nil:
			return fmt.Errorf("failed to get CRD %s: %w", crd.Name, err)
		default:
			log.Printf("Updating CRD: %s", crd.Name)
			if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				existingCRD, err := crdClient.Get(ctx, crd.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				crd.ResourceVersion = existingCRD.ResourceVersion
				_, err = crdClient.Update(ctx, crd, metav1.UpdateOptions{})
				return err
			}); err != nil {
				return fmt.Errorf("failed to update CRD %s: %w", crd.Name, err)
			}
		}
	}

	return nil
}

// deleteCRDs removes CRDs from the cluster.
func deleteCRDs(
	ctx context.Context,
	client *clientset.Clientset,
	crds []*apiextensionsv1.CustomResourceDefinition,
) error {
	crdClient := client.ApiextensionsV1().CustomResourceDefinitions()

	for _, crd := range crds {
		log.Printf("Deleting CRD: %s", crd.Name)
		err := crdClient.Delete(ctx, crd.Name, metav1.DeleteOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Printf("CRD does not exist, skipping: %s", crd.Name)
			} else {
				return fmt.Errorf("failed to delete CRD %s: %w", crd.Name, err)
			}
		}
	}

	return nil
}

// waitForCRDs waits for CRDs to be established in the API server.
func waitForCRDs(ctx context.Context, config *rest.Config, crds []*apiextensionsv1.CustomResourceDefinition) error {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create discovery client: %w", err)
	}

	for _, crd := range crds {
		log.Printf("Waiting for CRD to be ready: %s", crd.Name)

		pollInterval := 100 * time.Millisecond
		pollTimeout := 10 * time.Second
		err := wait.PollUntilContextTimeout(ctx, pollInterval, pollTimeout, true,
			func(_ context.Context) (bool, error) {
				// Check if the CRD's group version is available in the API server
				for _, version := range crd.Spec.Versions {
					if !version.Served {
						continue
					}

					gv := crd.Spec.Group + "/" + version.Name
					resourceList, err := discoveryClient.ServerResourcesForGroupVersion(gv)
					// Resource not found yet or transient error, keep polling
					if apierrors.IsNotFound(err) || apierrors.IsServiceUnavailable(err) {
						return false, nil
					} else if err != nil {
						return false, err
					}

					for _, resource := range resourceList.APIResources {
						if resource.Name == crd.Spec.Names.Plural {
							return true, nil
						}
					}
				}

				return false, nil
			})

		if err != nil {
			return fmt.Errorf("CRD %s failed to become ready: %w", crd.Name, err)
		}
	}

	return nil
}
