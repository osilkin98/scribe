package controllers

import (
	// "context"
	// "time"

	"context"
	// "fmt"

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

		// need secret for most of these tests to work
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
		rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{
			ReplicationDestinationVolumeOptions: scribev1alpha1.ReplicationDestinationVolumeOptions{
				CopyMethod: scribev1alpha1.CopyMethodNone,
			},
			RcloneConfigSection: &configSection,
			RcloneDestPath:      &destPath,
			RcloneConfig:        &rcloneSecret.Name,
		}
		RcloneContainerImage = DefaultRcloneContainerImage
	})
	AfterEach(func() {
		// delete each namespace on shutdown so resources can be reclaimed
		Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
	})
	JustBeforeEach(func() {
		// source pvc comes up
		Expect(k8sClient.Create(ctx, rcloneSecret)).To(Succeed())
		Expect(k8sClient.Create(ctx, rd)).To(Succeed())
		// wait for the ReplicationDestination to actually come up
		Eventually(func() error {
			inst := &scribev1alpha1.ReplicationDestination{}
			return k8sClient.Get(ctx, nameFor(rd), inst)
		}, maxWait, interval).Should(Succeed())
	})

	// ***************************** this fails

	//nolint:dupl
	Context("when a destinationPVC is specified", func() {
		var pvc *corev1.PersistentVolumeClaim
		BeforeEach(func() {
			pvc = &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: rd.Namespace,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"storage": resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
			rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{
				ReplicationDestinationVolumeOptions: scribev1alpha1.ReplicationDestinationVolumeOptions{
					DestinationPVC: &pvc.Name,
				},
				RcloneConfigSection: &configSection,
				RcloneDestPath:      &destPath,
				RcloneConfig:        &rcloneSecret.Name,
			}
		})

		It("Test if job finishes", func() {
			//job := &batchv1.Job{}
			Eventually(func() error {
				inst := &scribev1alpha1.ReplicationDestination{}
				return k8sClient.Get(ctx, nameFor(rd), inst)
			}, maxWait, interval).Should(Succeed())
			Expect(true).To(BeTrue())
			Expect(pvc).NotTo(beOwnedBy(rd))
		})
	})

	Context("When a schedule is provided", func() {
		var schedule = "*/1 * * * *"
		BeforeEach(func() {
			capacity := resource.MustParse("2Gi")
			accessModes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
			rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{
				ReplicationDestinationVolumeOptions: scribev1alpha1.ReplicationDestinationVolumeOptions{
					Capacity:    &capacity,
					AccessModes: accessModes,
					CopyMethod:  scribev1alpha1.CopyMethodSnapshot,
				},
				RcloneDestPath:      &destPath,
				RcloneConfig:        &rcloneSecret.Name,
				RcloneConfigSection: &configSection,
			}
			rd.Spec.Trigger = &scribev1alpha1.ReplicationDestinationTriggerSpec{
				Schedule: &schedule,
			}
		})

		It("Provided a schedule", func() {
			Eventually(func() error {
				inst := &scribev1alpha1.ReplicationDestination{}
				return k8sClient.Get(ctx, nameFor(rd), inst)
			}, maxWait, interval).Should(Succeed())
			Expect(rd.Status).NotTo(BeNil())
			Expect(rd.Status)
			Expect(rd.Status.LatestImage).NotTo(BeNil())
			Expect(rd.Status.LatestImage.Name).NotTo(BeEmpty())
		})
	})

	/** start with covering ensureJob **/
	/** create a PR **/
})
