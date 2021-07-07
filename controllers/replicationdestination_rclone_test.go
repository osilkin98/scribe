package controllers

import (
	"context"
	"encoding/json"
	"fmt"
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
	"k8s.io/apimachinery/pkg/types"
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
	var job *batchv1.Job
	var schedule = "*/4 * * * *"
	var capacity = resource.MustParse("2Gi")

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
						"storage": resource.MustParse("2Gi"),
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
		// scaffolded ReplicationDestination - extra fields will be set in subsequent tests
		rd = &scribev1alpha1.ReplicationDestination{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "instance",
				Namespace: namespace.Name,
			},
		}
		// setup a minimal job
		job = &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "scribe-rclone-src-" + rd.Name,
				Namespace: rd.Namespace,
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

	When("ReplicationDestinationRcloneSpec is empty", func() {
		BeforeEach(func() {
			rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{}
		})
	})

	//nolint:dupl
	When("ReplicationDestination is provided with a minimal rclone spec", func() {
		BeforeEach(func() {
			// uses copymethod + accessModes instead
			rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{
				ReplicationDestinationVolumeOptions: scribev1alpha1.ReplicationDestinationVolumeOptions{},
				RcloneConfigSection:                 &configSection,
				RcloneDestPath:                      &destPath,
				RcloneConfig:                        &rcloneSecret.Name,
			}
			rd.Spec.Trigger = &scribev1alpha1.ReplicationDestinationTriggerSpec{
				Schedule: &schedule,
			}
		})

		It("should not start", func() {
			Eventually(func() error {
				return k8sClient.Get(ctx, nameFor(rd), rd)
			}, duration, interval).Should(Succeed())
			rdJson, _ := json.Marshal(rd)
			fmt.Printf("[mr boombastic]: %s\n\n", string(rdJson))
		})

		When("ReplicationDestination is provided AccessModes & Capacity", func() {
			BeforeEach(func() {
				rd.Spec.Rclone.ReplicationDestinationVolumeOptions = scribev1alpha1.ReplicationDestinationVolumeOptions{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Capacity:    &capacity,
				}
			})

			JustBeforeEach(func() {
				// force job to succeed
				Eventually(func() error {
					return k8sClient.Get(ctx, nameFor(job), job)
				}, maxWait, interval).Should(Succeed())
				job.Status.Succeeded = 1
				job.Status.StartTime = &metav1.Time{ // provide job with a start time
					Time: time.Now(),
				}
				Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())
			})

			When("Using a CopyMethod of None", func() {
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

				When("The ReplicationDestinaton spec is paused", func() {
					parallelism := int32(0)
					BeforeEach(func() {
						rd.Spec.Paused = true
					})
					It("Job has parallelism disabled", func() {
						job := &batchv1.Job{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "scribe-rclone-src-" + rd.Name,
								Namespace: rd.Namespace,
							},
						}
						Eventually(func() error {
							return k8sClient.Get(ctx, nameFor(job), job)
						}, maxWait, interval).Should(Succeed())
						Expect(*job.Spec.Parallelism).To(Equal(parallelism))
					})
				})

				When("A Storage Class is specified", func() {
					scName := "mysc"
					BeforeEach(func() {
						rd.Spec.Rclone.ReplicationDestinationVolumeOptions.StorageClassName = &scName
					})
					It("Is used in the destination PVC", func() {
						Expect(k8sClient.Get(ctx, nameFor(job), job)).To(Succeed())
						var pvcName string
						volumes := job.Spec.Template.Spec.Volumes
						for _, v := range volumes {
							if v.PersistentVolumeClaim != nil && v.Name == dataVolumeName {
								pvcName = v.PersistentVolumeClaim.ClaimName
							}
						}
						pvc = &corev1.PersistentVolumeClaim{}
						Eventually(func() error {
							return k8sClient.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: rd.Namespace}, pvc)
						}, maxWait, interval).Should(Succeed())
						Expect(*pvc.Spec.StorageClassName).To(Equal(scName))
					})
				})
			})

			When("Using a copy method of Snapshot", func() {
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

				When("When a VolumeSnapshotClass is specified", func() {
					vscName := "MyVolumeSnapshotClass"
					BeforeEach(func() {
						rd.Spec.Rclone.ReplicationDestinationVolumeOptions.VolumeSnapshotClassName = &vscName
					})

					It("is used as the VSC for the Snapshot", func() {
						// create a snapshot & verify the VSC matches
						snapshots := &snapv1.VolumeSnapshotList{}
						Eventually(func() []snapv1.VolumeSnapshot {
							_ = k8sClient.List(ctx, snapshots, client.InNamespace(rd.Namespace))
							return snapshots.Items
						}, maxWait, interval).Should(Not(BeEmpty()))
						snapshot := snapshots.Items[0]
						Expect(*snapshot.Spec.VolumeSnapshotClassName).To(Equal(vscName))
					})
				})
			})

			It("Job set to fail", func() {
				Eventually(func() error {
					return k8sClient.Get(ctx, nameFor(job), job)
				}, maxWait, interval).Should(Succeed())
				// force job fail state
				job.Status.Failed = *job.Spec.BackoffLimit + 1000
				Eventually(func() error {
					return k8sClient.Status().Update(ctx, job)
				}, duration, interval).Should(Succeed())
				// job should eventually be restarted
				Eventually(func() error {
					return k8sClient.Get(ctx, nameFor(job), job)
				}, maxWait, interval).Should(Succeed())
			})
		})
	})

	When("The secret is sus", func() {
		Context("Secret isn't provided with the correct fields", func() {
			BeforeEach(func() {
				// setup a sus secret
				rcloneSecret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "rclone-secret",
						Namespace: namespace.Name,
					},
					StringData: map[string]string{
						"field-redacted": "this data is trash",
					},
				}
				rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{
					ReplicationDestinationVolumeOptions: scribev1alpha1.ReplicationDestinationVolumeOptions{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Capacity:    &capacity,
					},
					RcloneConfigSection: &configSection,
					RcloneDestPath:      &destPath,
					RcloneConfig:        &rcloneSecret.Name,
				}
			})
			It("Reconcile Condition is set to false", func() {
				// make sure that the condition is set to not be reconciled
				inst := &scribev1alpha1.ReplicationDestination{}
				// wait for replicationdestination to have a status
				Eventually(func() *scribev1alpha1.ReplicationDestinationStatus {
					_ = k8sClient.Get(ctx, nameFor(rd), inst)
					return inst.Status
				}, duration, interval).Should(Not(BeNil()))
				Expect(inst.Status.Conditions).ToNot(BeEmpty())
				reconcileCondition := inst.Status.Conditions.GetCondition(scribev1alpha1.ConditionReconciled)
				Expect(reconcileCondition).ToNot(BeNil())
				Expect(reconcileCondition.Status).To(Equal(corev1.ConditionFalse))
			})
		})

		// test each of the possible configurations
		Context("Secret fields are zero-length", func() {
			BeforeEach(func() {
				// initialize all config sections to zero length
				var zeroLength = ""
				rd.Spec.Rclone = &scribev1alpha1.ReplicationDestinationRcloneSpec{
					ReplicationDestinationVolumeOptions: scribev1alpha1.ReplicationDestinationVolumeOptions{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Capacity:    &capacity,
					},
					RcloneConfigSection: &zeroLength,
					RcloneDestPath:      &zeroLength,
					RcloneConfig:        &zeroLength,
				}
				rd.Spec.Trigger = &scribev1alpha1.ReplicationDestinationTriggerSpec{
					Schedule: &schedule,
				}
				// setup a minimal job
				job = &batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "scribe-rclone-src-" + rd.Name,
						Namespace: rd.Namespace,
					},
				}
			})
			It("No spec at all", func() {
				Consistently(func() error {
					return k8sClient.Get(ctx, nameFor(job), job)
				}, time.Second, interval).ShouldNot(Succeed())
			})

			When("Some of the config sections are provided", func() {
				Context("RcloneConfig", func() {
					BeforeEach(func() {
						rd.Spec.Rclone.RcloneConfig = &rcloneSecret.Name
					})
					It("RcloneConfig is provided", func() {
						Consistently(func() error {
							return k8sClient.Get(ctx, nameFor(job), job)
						}, time.Second, interval).ShouldNot(Succeed())
					})
				})
				Context("RcloneConfig + RcloneConfigSection", func() {
					BeforeEach(func() {
						rd.Spec.Rclone.RcloneConfig = &rcloneSecret.Name
						rd.Spec.Rclone.RcloneConfigSection = &configSection
					})
					It("RcloneConfig & RcloneConfigSection are set to non-nil", func() {
						Consistently(func() error {
							return k8sClient.Get(ctx, nameFor(job), job)
						}, time.Second, interval).ShouldNot(Succeed())
					})
				})
				Context("Everything is provided", func() {
					BeforeEach(func() {
						rd.Spec.Rclone.RcloneConfig = &rcloneSecret.Name
						rd.Spec.Rclone.RcloneConfigSection = &configSection
						rd.Spec.Rclone.RcloneDestPath = &destPath
					})
					It("Job successfully starts", func() {
						Eventually(func() error {
							return k8sClient.Get(ctx, nameFor(job), job)
						}, maxWait, interval).Should(Succeed())
					})
				})
			})
		})
	})

	// todo: test failure conditions
	// 		- job failing & gracefully being deleted for recreation
	//		- failing to update last sync destination on cleanup
	//
	// todo: ensureJob
	//		- failing to set controller reference
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

	/** start with covering ensureJob **/
	/** create a PR **/
})
