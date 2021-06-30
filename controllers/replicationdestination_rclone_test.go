package controllers

import (
	"context"
	"fmt"

	// snapv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	// "github.com/operator-framework/operator-lib/status"
	//batchv1 "k8s.io/api/batch/v1"

	scribev1alpha1 "github.com/backube/scribe/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//nolint:dupl
var _ = Describe("ReplicationDestination [rclone]", func() {
	var ctx = context.Background()
	var namespace *corev1.Namespace
	var rd *scribev1alpha1.ReplicationDestination
	var rcloneSecret *corev1.Secret
	var configSection = "foo"
	var destPath = "bar"
	var pvc *corev1.PersistentVolumeClaim
	var schedule = "*/4 * * * *"

	// setup namespace && PVC
	BeforeEach(func() {
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "scribe-rclone-dest-",
			},
		}
		// crete ns
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		Expect(namespace.Name).NotTo(BeEmpty())

		// sets up RD, Rclone, Secret & PVC spec
		pvc = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: namespace.Name,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"storage": resource.MustParse("10Gi"),
					},
				},
			},
		}
		rcloneSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rclone-secret",
				Namespace: namespace.Name,
			},
			StringData: map[string]string{
				"rclone.conf": "hunter2",
			},
		}
		rd = &scribev1alpha1.ReplicationDestination{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "instance",
				Namespace: namespace.Name,
			},
		}
		RcloneContainerImage = DefaultRcloneContainerImage
	})
	AfterEach(func() {
		// delete each namespace on shutdown so resources can be reclaimed
		Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
	})
	JustBeforeEach(func() {
		// create necessary services
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
		Expect(k8sClient.Create(ctx, rcloneSecret)).To(Succeed())
		Expect(k8sClient.Create(ctx, rd)).To(Succeed())
		// wait for the ReplicationDestination to actually come up
		Eventually(func() error {
			inst := &scribev1alpha1.ReplicationDestination{}
			return k8sClient.Get(ctx, nameFor(rd), inst)
		}, maxWait, interval).Should(Succeed())
	})

	//nolint:dupl
	Context("When ReplicationDestination is provided with a minimal rclone spec", func() {
		// var pvc *corev1.PersistentVolumeClaim
		// BeforeEach(func() {
		// 	Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
		// 	rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{
		// 		ReplicationDestinationVolumeOptions: scribev1alpha1.ReplicationDestinationVolumeOptions{
		// 			DestinationPVC: &pvc.Name,
		// 		},
		// 		RcloneConfigSection: &configSection,
		// 		RcloneDestPath:      &destPath,
		// 		RcloneConfig:        &rcloneSecret.Name,
		// 	}
		// })

		BeforeEach(func() {
			capacity := resource.MustParse("2Gi")
			// uses copymethod + accessModes instead
			rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{
				ReplicationDestinationVolumeOptions: scribev1alpha1.ReplicationDestinationVolumeOptions{
					CopyMethod:  scribev1alpha1.CopyMethodSnapshot,
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Capacity:    &capacity,
				},
				RcloneConfigSection: &configSection,
				RcloneDestPath:      &destPath,
				RcloneConfig:        &rcloneSecret.Name,
			}
			rd.Spec.Trigger = &scribev1alpha1.ReplicationDestinationTriggerSpec{
				Schedule: &schedule,
			}
		})

		It("Ensure that ReplicationDestination starts", func() {
			//job := &batchv1.Job{}
			Eventually(func() error {
				inst := &scribev1alpha1.ReplicationDestination{}
				return k8sClient.Get(ctx, nameFor(rd), inst)
			}, maxWait, interval).Should(Succeed())
			Eventually(func() bool {
				inst := &scribev1alpha1.ReplicationDestination{}
				if err := k8sClient.Get(ctx, nameFor(rd), inst); err != nil {
					return false
				}
				if inst.Status == nil {
					return false
				} else {
					return true
				}
			}, maxWait, interval).Should(BeTrue())
			inst := &scribev1alpha1.ReplicationDestination{}
			Expect(k8sClient.Get(ctx, nameFor(rd), inst)).To(Succeed())
			Expect(inst.Status).NotTo(BeNil())
			Expect(inst.Status.Conditions).NotTo(BeNil())
			Expect(inst.Status.NextSyncTime).NotTo(BeNil())
		})

		It("Wait until ReplicationDestination syncs", func() {
			inst := &scribev1alpha1.ReplicationDestination{}
			Expect(k8sClient.Get(ctx, nameFor(rd), inst)).To(Succeed())
			fmt.Printf("*************\n*************inst: %+v\n*****\n*****", rd.Status)
			Eventually(func() bool {
				fmt.Printf("\n\npolling...\n\n")
				inst := &scribev1alpha1.ReplicationDestination{}
				err := k8sClient.Get(ctx, nameFor(rd), inst)
				if err != nil {
					fmt.Printf("got error: %+v returning false\n\n", err)
					return false
				}

				if inst.Status != nil && inst.Status.LastSyncTime != nil {
					fmt.Printf("lastSyncTime exists, returning true\n\n")
					return true
				}
				return false
			}, maxWait, interval).Should(BeTrue())
		})
	})

	// Context("When a schedule is provided", func() {
	// 	var schedule = "*/1 * * * *"
	// 	BeforeEach(func() {
	// 		capacity := resource.MustParse("2Gi")
	// 		accessModes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	// 		rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{
	// 			ReplicationDestinationVolumeOptions: scribev1alpha1.ReplicationDestinationVolumeOptions{
	// 				Capacity:    &capacity,
	// 				AccessModes: accessModes,
	// 				CopyMethod:  scribev1alpha1.CopyMethodSnapshot,
	// 			},
	// 			RcloneDestPath:      &destPath,
	// 			RcloneConfig:        &rcloneSecret.Name,
	// 			RcloneConfigSection: &configSection,
	// 		}
	// 		rd.Spec.Trigger = &scribev1alpha1.ReplicationDestinationTriggerSpec{
	// 			Schedule: &schedule,
	// 		}
	// 	})

	// 	It("Provided a schedule", func() {
	// 		Eventually(func() error {
	// 			inst := &scribev1alpha1.ReplicationDestination{}
	// 			return k8sClient.Get(ctx, nameFor(rd), inst)
	// 		}, maxWait, interval).Should(Succeed())
	// 		Expect(rd.Status).NotTo(BeNil())
	// 		Expect(rd.Status)
	// 		Expect(rd.Status.LatestImage).NotTo(BeNil())
	// 		Expect(rd.Status.LatestImage.Name).NotTo(BeEmpty())
	// 	})
	// })

	/** start with covering ensureJob **/
	/** create a PR **/
})
