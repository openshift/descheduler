/*
Copyright 2021 The Kubernetes Authors.

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

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/component-base/config"
	"k8s.io/utils/ptr"

	storagev1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/descheduler/cmd/descheduler/app/options"
	"sigs.k8s.io/descheduler/pkg/api"
	apiv1alpha2 "sigs.k8s.io/descheduler/pkg/api/v1alpha2"
	"sigs.k8s.io/descheduler/pkg/descheduler/client"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/defaultevictor"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodshavingtoomanyrestarts"
)

// protectPodsWithPVCPolicy returns a descheduler policy that protects pods
// using PVCs of specific storage classes from eviction while, at the same
// time, evicting pods that have restarted more than 3 times.
func protectPodsWithPVCPolicy(namespace string, protectedsc []defaultevictor.ProtectedStorageClass) *apiv1alpha2.DeschedulerPolicy {
	return &apiv1alpha2.DeschedulerPolicy{
		Profiles: []apiv1alpha2.DeschedulerProfile{
			{
				Name: "ProtectPodsWithPVCPolicy",
				PluginConfigs: []apiv1alpha2.PluginConfig{
					{
						Name: removepodshavingtoomanyrestarts.PluginName,
						Args: runtime.RawExtension{
							Object: &removepodshavingtoomanyrestarts.RemovePodsHavingTooManyRestartsArgs{
								PodRestartThreshold:     3,
								IncludingInitContainers: true,
								Namespaces: &api.Namespaces{
									Include: []string{namespace},
								},
							},
						},
					},
					{
						Name: defaultevictor.PluginName,
						Args: runtime.RawExtension{
							Object: &defaultevictor.DefaultEvictorArgs{
								PodProtections: defaultevictor.PodProtections{
									DefaultDisabled: []defaultevictor.PodProtection{
										defaultevictor.PodsWithLocalStorage,
									},
									ExtraEnabled: []defaultevictor.PodProtection{
										defaultevictor.PodsWithPVC,
									},
									Config: &defaultevictor.PodProtectionsConfig{
										PodsWithPVC: &defaultevictor.PodsWithPVCConfig{
											ProtectedStorageClasses: protectedsc,
										},
									},
								},
							},
						},
					},
				},
				Plugins: apiv1alpha2.Plugins{
					Filter: apiv1alpha2.PluginSet{
						Enabled: []string{
							defaultevictor.PluginName,
						},
					},
					Deschedule: apiv1alpha2.PluginSet{
						Enabled: []string{
							removepodshavingtoomanyrestarts.PluginName,
						},
					},
				},
			},
		},
	}
}

// TestProtectPodsWithPVC tests that pods using PVCs are protected.
// This test requires a modern descheduler image that understands the podProtections
// field in DefaultEvictorArgs. Older images (targeted by --legacy-evictor=true) use
// strict YAML decoding and reject unknown fields, so the descheduler pod would fail
// to start. Skip in legacy mode rather than attempt to run with an incompatible image.
func TestProtectPodsWithPVC(t *testing.T) {
	if *legacyEvictor {
		t.Skip("TestProtectPodsWithPVC requires podProtections support in DefaultEvictorArgs; skip with --legacy-evictor=true (older descheduler images)")
	}
	ctx := context.Background()
	initPluginRegistry()

	cli, err := client.CreateClient(
		config.ClientConnectionConfiguration{
			Kubeconfig: os.Getenv("KUBECONFIG"),
		}, "",
	)
	if err != nil {
		t.Fatalf("error during kubernetes client creation with %v", err)
	}

	// start by finding out what is the default storage class in the
	// cluster. if none is found then this test can't run.
	scs, err := cli.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("error listing storage classes: %v", err)
	}

	var defclass *storagev1.StorageClass
	for _, sc := range scs.Items {
		if _, ok := sc.Annotations["storageclass.kubernetes.io/is-default-class"]; ok {
			defclass = &sc
			break
		}
	}
	if defclass == nil {
		t.Fatalf("no default storage class found, unable to run the test")
	}

	// now we replicate the default storage class so we have two different
	// storage classes in the cluster. this is useful to test protected vs
	// unprotected pods using PVCs.
	unprotectedsc := defclass.DeepCopy()
	delete(unprotectedsc.Annotations, "storageclass.kubernetes.io/is-default-class")
	unprotectedsc.ResourceVersion = ""
	unprotectedsc.Name = "unprotected"
	if _, err = cli.StorageV1().StorageClasses().Create(ctx, unprotectedsc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("error creating unprotected storage class: %v", err)
	}
	defer cli.StorageV1().StorageClasses().Delete(ctx, unprotectedsc.Name, metav1.DeleteOptions{})

	// this is the namespace we are going to use for all testing
	t.Logf("creating testing namespace %v", t.Name())
	namespace := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("e2e-%s", strings.ToLower(t.Name())),
		},
	}
	if _, err := cli.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Unable to create ns %v", namespace.Name)
	}
	defer cli.CoreV1().Namespaces().Delete(ctx, namespace.Name, metav1.DeleteOptions{})

	for _, tc := range []struct {
		name                    string
		policy                  *apiv1alpha2.DeschedulerPolicy
		enableGracePeriod       bool
		expectedEvictedPodCount uint
		pvcs                    []*v1.PersistentVolumeClaim
		volumes                 []v1.Volume
	}{
		{
			name: "evict pods from unprotected storage class",
			policy: protectPodsWithPVCPolicy(
				namespace.Name, []defaultevictor.ProtectedStorageClass{
					{
						Name: defclass.Name,
					},
				},
			),
			expectedEvictedPodCount: 4,
			pvcs: []*v1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-unprotected-claim",
						Namespace: namespace.Name,
					},
					Spec: v1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To(unprotectedsc.Name),
						AccessModes: []v1.PersistentVolumeAccessMode{
							v1.ReadWriteOnce,
						},
						Resources: v1.VolumeResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			volumes: []v1.Volume{
				{
					Name: "test-unprotected-volume",
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-unprotected-claim",
						},
					},
				},
			},
		},
		{
			name: "preserve pods from protected storage class",
			policy: protectPodsWithPVCPolicy(
				namespace.Name, []defaultevictor.ProtectedStorageClass{
					{
						Name: defclass.Name,
					},
				},
			),
			expectedEvictedPodCount: 0,
			pvcs: []*v1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-protected-claim",
						Namespace: namespace.Name,
					},
					Spec: v1.PersistentVolumeClaimSpec{
						AccessModes: []v1.PersistentVolumeAccessMode{
							v1.ReadWriteOnce,
						},
						Resources: v1.VolumeResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			volumes: []v1.Volume{
				{
					Name: "test-protected-volume",
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: "test-protected-claim",
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("creating testing pvcs in namespace %v", namespace.Name)
			for _, pvc := range tc.pvcs {
				if _, err = cli.CoreV1().PersistentVolumeClaims(namespace.Name).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
					t.Fatalf("error creating PVC: %v", err)
				}
				defer cli.CoreV1().PersistentVolumeClaims(namespace.Name).Delete(ctx, pvc.Name, metav1.DeleteOptions{})
			}

			runAsUser, runAsGroup, uidErr := getRunAsForNamespace(ctx, cli, namespace.Name)
			if uidErr != nil {
				t.Fatalf("getRunAsForNamespace(%q): %v", namespace.Name, uidErr)
			}
			deploy := buildTestDeployment(
				"restart-pod",
				namespace.Name,
				4,
				map[string]string{"test": "restart-pod", "name": "test-toomanyrestarts"},
				func(deployment *appsv1.Deployment) {
					deployment.Spec.Template.Spec.Containers[0].Command = []string{"/bin/sh"}
					deployment.Spec.Template.Spec.Containers[0].Args = []string{"-c", "sleep 1s && exit 1"}
				},
				&runAsUser, &runAsGroup,
			)
			deploy.Spec.Template.Spec.Volumes = tc.volumes

			t.Logf("creating deployment %v", deploy.Name)
			if _, err := cli.AppsV1().Deployments(deploy.Namespace).Create(ctx, deploy, metav1.CreateOptions{}); err != nil {
				t.Fatalf("error creating deployment: %v", err)
			}
			defer cli.AppsV1().Deployments(deploy.Namespace).Delete(ctx, deploy.Name, metav1.DeleteOptions{})

			// Wait for all PVCs to reach ClaimBound now that consumer pods have been
			// scheduled. This is safe for both Immediate and WaitForFirstConsumer
			// volume binding modes: under WaitForFirstConsumer the PV is only
			// provisioned once a consumer pod is scheduled, so we must create the
			// deployment first and check binding afterwards.
			for _, pvcCheck := range tc.pvcs {
				pvcName := pvcCheck.Name
				if err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
					vol, err := cli.CoreV1().PersistentVolumeClaims(namespace.Name).Get(ctx, pvcName, metav1.GetOptions{})
					if err != nil {
						return false, err
					}
					if vol.Status.Phase == v1.ClaimBound {
						return true, nil
					}
					t.Logf("waiting for PVC %s/%s to bind, phase=%s", namespace.Name, pvcName, vol.Status.Phase)
					return false, nil
				}); err != nil {
					t.Fatalf("PVC %s/%s did not reach Bound: %v", namespace.Name, pvcName, err)
				}
			}

			// wait for 3 restarts
			waitPodRestartCount(ctx, cli, namespace.Name, t, 3)

			rs, err := options.NewDeschedulerServer()
			if err != nil {
				t.Fatalf("unable to initialize server: %v\n", err)
			}
			rs.Client, rs.EventClient, rs.DefaultFeatureGates = cli, cli, initFeatureGates()
			preRunNames := sets.NewString(getCurrentPodNames(ctx, cli, namespace.Name, t)...)

			// deploy the descheduler with the configured policy
			// Use deschedulerPolicyConfigMapFull so that podProtections.config is preserved
			// in the serialised policy. The regular deschedulerPolicyConfigMap strips it for
			// legacy-image compatibility, which would silently disable the protection logic
			// this test is specifically exercising.
			policycm, err := deschedulerPolicyConfigMapFull(tc.policy)
			if err != nil {
				t.Fatalf("Error creating %q CM: %v", policycm.Name, err)
			}

			t.Logf("creating %q policy CM with PodsWithPVC protection enabled...", policycm.Name)
			if _, err = cli.CoreV1().ConfigMaps(policycm.Namespace).Create(
				ctx, policycm, metav1.CreateOptions{},
			); err != nil {
				t.Fatalf("error creating %q CM: %v", policycm.Name, err)
			}

			defer func() {
				t.Logf("deleting %q CM...", policycm.Name)
				if err := cli.CoreV1().ConfigMaps(policycm.Namespace).Delete(
					ctx, policycm.Name, metav1.DeleteOptions{},
				); err != nil {
					t.Fatalf("unable to delete %q CM: %v", policycm.Name, err)
				}
			}()

			runAsU, runAsG, uidErr := getRunAsForNamespace(ctx, cli, "kube-system")
			if uidErr != nil {
				t.Fatalf("getRunAsForNamespace(%q): %v", "kube-system", uidErr)
			}
			desdep := deschedulerDeployment(namespace.Name, &runAsU, &runAsG)
			t.Logf("creating descheduler deployment %v", desdep.Name)
			if _, err := createDeschedulerDeployment(ctx, t, cli, desdep); err != nil {
				t.Fatalf("error creating %q deployment: %v", desdep.Name, err)
			}

			deschedulerPodName := ""
			defer func() {
				if deschedulerPodName != "" {
					printPodLogs(ctx, t, cli, deschedulerPodName)
				}

				t.Logf("deleting %q deployment...", desdep.Name)
				if err := cli.AppsV1().Deployments(desdep.Namespace).Delete(
					ctx, desdep.Name, metav1.DeleteOptions{},
				); err != nil {
					t.Fatalf("unable to delete %q deployment: %v", desdep.Name, err)
				}

				waitForPodsToDisappear(ctx, t, cli, desdep.Labels, desdep.Namespace)
			}()

			t.Logf("waiting for the descheduler pod running")
			deschedulerPods := waitForPodsRunning(ctx, t, cli, desdep.Labels, 1, desdep.Namespace)
			if len(deschedulerPods) != 0 {
				deschedulerPodName = deschedulerPods[0].Name
			}

			// For tests that expect evictions (expectedEvictedPodCount > 0): poll until
			// the expected number of pods have been removed — the count rising to the
			// target is proof the descheduler acted.
			//
			// For tests that expect zero evictions (protected case): we MUST NOT return
			// success on the very first poll tick — at that point the descheduler simply
			// has not had time to run a cycle yet, so zero evictions proves nothing.
			// Instead: first wait for the descheduler to log "Number of evictions/requests"
			// which is emitted at V(1) at the end of every descheduling cycle (the
			// descheduler is started with --v 4 so this line always appears).  Only after
			// confirming at least one full cycle has completed do we assert the count.
			if tc.expectedEvictedPodCount == 0 {
				// Step 1: wait for at least one completed descheduler cycle.
				// We MUST verify a cycle ran before asserting zero evictions; a
				// stalled or failed descheduler also produces zero evictions and
				// would give a false-positive.  The log line
				// "Number of evictions/requests" is emitted at V(1) at the end of
				// every descheduling cycle — the descheduler runs with --v 4 so it
				// always appears.  Fail immediately if we cannot obtain a pod name
				// or if the cycle marker does not appear within the timeout; both
				// conditions indicate the descheduler did not run as expected.
				t.Logf("protected case: waiting for descheduler to complete at least one cycle before asserting zero evictions")
				if deschedulerPodName == "" {
					t.Fatal("descheduler pod name unavailable after waitForPodsRunning — cannot verify a descheduling cycle ran; failing to avoid a false-positive zero-eviction result")
				}
				if err := wait.PollUntilContextTimeout(ctx, 3*time.Second, 2*time.Minute, false, func(ctx context.Context) (bool, error) {
					req := cli.CoreV1().Pods("kube-system").GetLogs(deschedulerPodName, &v1.PodLogOptions{})
					logBytes, err := req.DoRaw(ctx)
					if err != nil {
						return false, nil // pod may not be fully ready yet; retry
					}
					return strings.Contains(string(logBytes), "Number of evictions/requests"), nil
				}); err != nil {
					t.Fatalf("descheduler cycle completion not confirmed via pod logs within 2 min (pod %q): %v — cannot assert zero evictions without proof the descheduler evaluated the pods", deschedulerPodName, err)
				}
				// Step 2: assert the count is still zero.
				podList, err := cli.CoreV1().Pods(namespace.Name).List(ctx, metav1.ListOptions{})
				if err != nil {
					t.Fatalf("error listing pods after descheduler cycle: %v", err)
				}
				names := sets.NewString()
				for _, item := range podList.Items {
					names.Insert(item.Name)
				}
				actualEvictedPodCount := uint(preRunNames.Difference(names).Len())
				if actualEvictedPodCount != 0 {
					t.Fatalf("expected 0 evictions (pods with PVC on protected storage class should be preserved), got %d", actualEvictedPodCount)
				}
				t.Logf("confirmed: 0 pods evicted after a full descheduler cycle — protection is working")
			} else {
				// For non-zero expected evictions: poll until the target count is reached.
				if err := wait.PollUntilContextTimeout(
					ctx, 5*time.Second, time.Minute, true,
					func(ctx context.Context) (bool, error) {
						podList, err := cli.CoreV1().Pods(namespace.Name).List(ctx, metav1.ListOptions{})
						if err != nil {
							t.Fatalf("error listing pods: %v", err)
						}
						names := sets.NewString()
						for _, item := range podList.Items {
							names.Insert(item.Name)
						}
						actualEvictedPodCount := uint(preRunNames.Difference(names).Len())
						if actualEvictedPodCount != tc.expectedEvictedPodCount {
							t.Logf("expecting exactly %v pod(s) evicted, got %v instead",
								tc.expectedEvictedPodCount, actualEvictedPodCount)
							return false, nil
						}
						return true, nil
					},
				); err != nil {
					t.Fatalf("error waiting for expected evictions: %v", err)
				}
			}

			waitForTerminatingPodsToDisappear(ctx, t, cli, namespace.Name)
		})
	}
}
