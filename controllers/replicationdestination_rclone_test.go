package controllers

import (
	"context"
	"time"

	// snapv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1beta1"
	snapv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// "github.com/operator-framework/operator-lib/status"
	//batchv1 "k8s.io/api/batch/v1"

	scribev1alpha1 "github.com/backube/scribe/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
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
		BeforeEach(func() {
			capacity := resource.MustParse("2Gi")
			// uses copymethod + accessModes instead
			rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{
				ReplicationDestinationVolumeOptions: scribev1alpha1.ReplicationDestinationVolumeOptions{
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

		JustBeforeEach(func() {
			// ensure that the Job will reach cleanupJob
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scribe-rclone-src-" + rd.Name,
					Namespace: rd.Namespace,
				},
			}
			Eventually(func() error {
				return k8sClient.Get(ctx, nameFor(job), job)
			}, maxWait, interval).Should(Succeed())
			job.Status.Succeeded = 1
			job.Status.StartTime = &metav1.Time{
				Time: time.Now(),
			}
			Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())
		})

		Context("Using a CopyMethod of None", func() {
			BeforeEach(func() {
				rd.Spec.Rclone.CopyMethod = scribev1alpha1.CopyMethodNone
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
					return inst.Status != nil
				}, maxWait, interval).Should(BeTrue())
				inst := &scribev1alpha1.ReplicationDestination{}
				Expect(k8sClient.Get(ctx, nameFor(rd), inst)).To(Succeed())
				Expect(inst.Status).NotTo(BeNil())
				Expect(inst.Status.Conditions).NotTo(BeNil())
				Expect(inst.Status.NextSyncTime).NotTo(BeNil())
			})

			It("Ensure LastSyncTime & LatestImage is set properly after reconcilation", func() {
				Eventually(func() bool {
					inst := &scribev1alpha1.ReplicationDestination{}
					err := k8sClient.Get(ctx, nameFor(rd), inst)
					if err != nil || inst.Status == nil {
						return false
					}
					return inst.Status.LastSyncTime != nil
				}, maxWait, interval).Should(BeTrue())
				// get ReplicationDestination
				inst := &scribev1alpha1.ReplicationDestination{}
				Expect(k8sClient.Get(ctx, nameFor(rd), inst)).To(Succeed())
				// ensure Status holds correct data
				Expect(inst.Status.LatestImage).NotTo(BeNil())
				latestImage := inst.Status.LatestImage
				Expect(latestImage.Kind).To(Equal("PersistentVolumeClaim"))
				Expect(*latestImage.APIGroup).To(Equal(""))
				Expect(latestImage.Name).NotTo(Equal(""))
			})

			It("Duration is set if job is successful", func() {
				// Make sure that LastSyncDuration gets set
				Eventually(func() *metav1.Duration {
					inst := &scribev1alpha1.ReplicationDestination{}
					_ = k8sClient.Get(ctx, nameFor(rd), inst)
					return inst.Status.LastSyncDuration
				}, maxWait, interval).Should(Not(BeNil()))
			})
		})
		Context("Using a copy method of Snapshot", func() {

			BeforeEach(func() {
				rd.Spec.Rclone.CopyMethod = scribev1alpha1.CopyMethodSnapshot
			})
			It("Ensure that a VolumeSnapshot is created at the end of an iteration", func() {
				snapshots := &snapv1.VolumeSnapshotList{}
				Eventually(func() []snapv1.VolumeSnapshot {
					_ = k8sClient.List(ctx, snapshots, client.InNamespace(rd.Namespace))
					return snapshots.Items
				}, maxWait, interval).Should(Not(BeEmpty()))
				snapshot := snapshots.Items[0]
				foo := "dummysnapshot"
				snapshot.Status = &snapv1.VolumeSnapshotStatus{
					BoundVolumeSnapshotContentName: &foo,
				}
				Expect(k8sClient.Status().Update(ctx, &snapshot)).To(Succeed())
				Eventually(func() *v1.TypedLocalObjectReference {
					_ = k8sClient.Get(ctx, nameFor(rd), rd)
					return rd.Status.LatestImage
				}, maxWait, interval).Should(Not(BeNil()))
				latestImage := rd.Status.LatestImage
				Expect(latestImage.Kind).To(Equal("VolumeSnapshot"))
				Expect(*latestImage.APIGroup).To(Equal(snapv1.SchemeGroupVersion.Group))
				Expect(latestImage).To(Not(Equal("")))
			})
		})
	})

	// todo: test failure conditions
	// 		- job failing & gracefully being deleted for recreation
	//		- rclone secret failing to validate
	//		- job cleanup with a nil start time
	//		- failing to update last sync destination on cleanup
	//
	// todo: ensureJob
	//		- failing to set controller reference
	//		- pausing rcloneDestReconciler -> r.Instance.Spec.Paused set to 1
	//		- r.job.Status.Failed >= *r.job.Spec.BackoffLimit
	//		- err := ctrlutil.CreateOrUpdate; err != nil
	//
	// todo: ensureRcloneConfig
	//		- getAndValidateSecret returning an error
	//
	// todo: awaitNextSyncDestination
	//		- updateNextSyncDestination returns an error
	//		- rd.Spec.Trigger != nil and rd.Spec.Trigger.Manual != "" and rd.Spec.Trigger.Manual == rd.Status.LastManualSync
	//
	// todo: updateNextSyncDestination
	//		- parser.Parse(Schedule) returns error
	//		- pastScheduleDeadline(schedule, rd.Status.LastSyncTime.Time, time.Now
	// 		- no schedule w/ manual trigger or schedule w/ manual trigger
	//

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
