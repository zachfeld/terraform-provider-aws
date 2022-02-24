package ec2

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/hashicorp/aws-sdk-go-base/v2/awsv1shim/v2/tfawserr"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/customdiff"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/create"
	"github.com/hashicorp/terraform-provider-aws/internal/flex"
	tfiam "github.com/hashicorp/terraform-provider-aws/internal/service/iam"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
)

func ResourceInstance() *schema.Resource {
	//lintignore:R011
	return &schema.Resource{
		Create: resourceInstanceCreate,
		Read:   resourceInstanceRead,
		Update: resourceInstanceUpdate,
		Delete: resourceInstanceDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		SchemaVersion: 1,
		MigrateState:  InstanceMigrateState,

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(10 * time.Minute),
			Update: schema.DefaultTimeout(10 * time.Minute),
			Delete: schema.DefaultTimeout(20 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"ami": {
				Type:         schema.TypeString,
				ForceNew:     true,
				Computed:     true,
				Optional:     true,
				AtLeastOneOf: []string{"ami", "launch_template"},
			},
			"arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"associate_public_ip_address": {
				Type:     schema.TypeBool,
				ForceNew: true,
				Computed: true,
				Optional: true,
			},
			"availability_zone": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"cpu_core_count": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"cpu_threads_per_core": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"credit_specification": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					if old == "1" && new == "0" {
						return true
					}
					return false
				},
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"cpu_credits": {
							Type:     schema.TypeString,
							Optional: true,
							DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
								// Only work with existing instances
								if d.Id() == "" {
									return false
								}
								// Only work with missing configurations
								if new != "" {
									return false
								}
								// Only work when already set in Terraform state
								if old == "" {
									return false
								}
								return true
							},
						},
					},
				},
			},
			"disable_api_termination": {
				Type:     schema.TypeBool,
				Optional: true,
				Computed: true,
			},
			"ebs_block_device": {
				Type:     schema.TypeSet,
				Optional: true,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"delete_on_termination": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  true,
							ForceNew: true,
						},
						"device_name": {
							Type:     schema.TypeString,
							Required: true,
							ForceNew: true,
						},
						"encrypted": {
							Type:     schema.TypeBool,
							Optional: true,
							Computed: true,
							ForceNew: true,
						},
						"iops": {
							Type:             schema.TypeInt,
							Optional:         true,
							Computed:         true,
							ForceNew:         true,
							DiffSuppressFunc: iopsDiffSuppressFunc,
						},
						"kms_key_id": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
							ForceNew: true,
						},
						"snapshot_id": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
							ForceNew: true,
						},
						"tags": tagsSchemaConflictsWith([]string{"volume_tags"}),
						"throughput": {
							Type:             schema.TypeInt,
							Optional:         true,
							Computed:         true,
							ForceNew:         true,
							DiffSuppressFunc: throughputDiffSuppressFunc,
						},
						"volume_id": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"volume_size": {
							Type:     schema.TypeInt,
							Optional: true,
							Computed: true,
							ForceNew: true,
						},
						"volume_type": {
							Type:         schema.TypeString,
							Optional:     true,
							Computed:     true,
							ForceNew:     true,
							ValidateFunc: validation.StringInSlice(ec2.VolumeType_Values(), false),
						},
					},
				},
				Set: func(v interface{}) int {
					var buf bytes.Buffer
					m := v.(map[string]interface{})
					buf.WriteString(fmt.Sprintf("%s-", m["device_name"].(string)))
					buf.WriteString(fmt.Sprintf("%s-", m["snapshot_id"].(string)))
					return create.StringHashcode(buf.String())
				},
			},
			"ebs_optimized": {
				Type:     schema.TypeBool,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"enclave_options": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"enabled": {
							Type:     schema.TypeBool,
							Optional: true,
							Computed: true,
							ForceNew: true,
						},
					},
				},
			},
			"ephemeral_block_device": {
				Type:     schema.TypeSet,
				Optional: true,
				Computed: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"device_name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"no_device": {
							Type:     schema.TypeBool,
							Optional: true,
						},
						"virtual_name": {
							Type:     schema.TypeString,
							Optional: true,
						},
					},
				},
				Set: func(v interface{}) int {
					var buf bytes.Buffer
					m := v.(map[string]interface{})
					buf.WriteString(fmt.Sprintf("%s-", m["device_name"].(string)))
					buf.WriteString(fmt.Sprintf("%s-", m["virtual_name"].(string)))
					if v, ok := m["no_device"].(bool); ok && v {
						buf.WriteString(fmt.Sprintf("%t-", v))
					}
					return create.StringHashcode(buf.String())
				},
			},
			"get_password_data": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			"hibernation": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: true,
			},
			"host_id": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"iam_instance_profile": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"instance_initiated_shutdown_behavior": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			"instance_state": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"instance_type": {
				Type:         schema.TypeString,
				Computed:     true,
				Optional:     true,
				AtLeastOneOf: []string{"instance_type", "launch_template"},
			},
			"ipv6_address_count": {
				Type:     schema.TypeInt,
				Optional: true,
				ForceNew: true,
				Computed: true,
			},
			"ipv6_addresses": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				ForceNew: true,
				Elem: &schema.Schema{
					Type:         schema.TypeString,
					ValidateFunc: validation.IsIPv6Address,
				},
			},
			"key_name": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Computed: true,
			},
			"launch_template": {
				Type:         schema.TypeList,
				MaxItems:     1,
				Optional:     true,
				ForceNew:     true,
				AtLeastOneOf: []string{"ami", "instance_type", "launch_template"},
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"id": {
							Type:         schema.TypeString,
							Optional:     true,
							Computed:     true,
							ForceNew:     true,
							ExactlyOneOf: []string{"launch_template.0.name", "launch_template.0.id"},
							ValidateFunc: verify.ValidLaunchTemplateID,
						},
						"name": {
							Type:         schema.TypeString,
							Optional:     true,
							Computed:     true,
							ForceNew:     true,
							ExactlyOneOf: []string{"launch_template.0.name", "launch_template.0.id"},
							ValidateFunc: verify.ValidLaunchTemplateName,
						},
						"version": {
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validation.StringLenBetween(1, 255),
							Default:      "$Default",
						},
					},
				},
			},
			"metadata_options": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"http_endpoint": {
							Type:         schema.TypeString,
							Optional:     true,
							Computed:     true,
							ValidateFunc: validation.StringInSlice([]string{ec2.InstanceMetadataEndpointStateEnabled, ec2.InstanceMetadataEndpointStateDisabled}, false),
						},
						"http_put_response_hop_limit": {
							Type:         schema.TypeInt,
							Optional:     true,
							Computed:     true,
							ValidateFunc: validation.IntBetween(1, 64),
						},
						"http_tokens": {
							Type:         schema.TypeString,
							Optional:     true,
							Computed:     true,
							ValidateFunc: validation.StringInSlice([]string{ec2.HttpTokensStateOptional, ec2.HttpTokensStateRequired}, false),
						},
						"instance_metadata_tags": {
							Type:         schema.TypeString,
							Optional:     true,
							Default:      ec2.InstanceMetadataTagsStateDisabled,
							ValidateFunc: validation.StringInSlice(ec2.InstanceMetadataTagsState_Values(), false),
						},
					},
				},
			},
			"monitoring": {
				Type:     schema.TypeBool,
				Optional: true,
				Computed: true,
			},
			"network_interface": {
				ConflictsWith: []string{"associate_public_ip_address", "subnet_id", "private_ip", "secondary_private_ips", "vpc_security_group_ids", "security_groups", "ipv6_addresses", "ipv6_address_count", "source_dest_check"},
				Type:          schema.TypeSet,
				Optional:      true,
				Computed:      true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"delete_on_termination": {
							Type:     schema.TypeBool,
							Default:  false,
							Optional: true,
							ForceNew: true,
						},
						"device_index": {
							Type:     schema.TypeInt,
							Required: true,
							ForceNew: true,
						},
						"network_interface_id": {
							Type:     schema.TypeString,
							Required: true,
							ForceNew: true,
						},
					},
				},
			},
			"outpost_arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"password_data": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"placement_group": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"placement_partition_number": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"primary_network_interface_id": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"private_dns": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"private_ip": {
				Type:         schema.TypeString,
				Optional:     true,
				ForceNew:     true,
				Computed:     true,
				ValidateFunc: validation.IsIPv4Address,
			},
			"public_dns": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"public_ip": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"root_block_device": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					// "For the root volume, you can only modify the following: volume size, volume type, and the Delete on Termination flag."
					// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/block-device-mapping-concepts.html
					Schema: map[string]*schema.Schema{
						"delete_on_termination": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  true,
						},
						"device_name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"encrypted": {
							Type:     schema.TypeBool,
							Optional: true,
							Computed: true,
							ForceNew: true,
						},
						"iops": {
							Type:             schema.TypeInt,
							Optional:         true,
							Computed:         true,
							DiffSuppressFunc: iopsDiffSuppressFunc,
						},
						"kms_key_id": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
							ForceNew: true,
						},
						"tags": tagsSchemaConflictsWith([]string{"volume_tags"}),
						"throughput": {
							Type:             schema.TypeInt,
							Optional:         true,
							Computed:         true,
							DiffSuppressFunc: throughputDiffSuppressFunc,
						},
						"volume_id": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"volume_size": {
							Type:     schema.TypeInt,
							Optional: true,
							Computed: true,
						},
						"volume_type": {
							Type:         schema.TypeString,
							Optional:     true,
							Computed:     true,
							ValidateFunc: validation.StringInSlice(ec2.VolumeType_Values(), false),
						},
					},
				},
			},
			"secondary_private_ips": {
				Type:     schema.TypeSet,
				Optional: true,
				Computed: true,
				Elem: &schema.Schema{
					Type:         schema.TypeString,
					ValidateFunc: validation.IsIPv4Address,
				},
			},
			"security_groups": {
				Type:     schema.TypeSet,
				Optional: true,
				Computed: true,
				ForceNew: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Set:      schema.HashString,
			},
			"source_dest_check": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  true,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					// Suppress diff if network_interface is set
					_, ok := d.GetOk("network_interface")
					return ok
				},
			},
			"subnet_id": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"tags":     tftags.TagsSchema(),
			"tags_all": tftags.TagsSchemaComputed(),
			"tenancy": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
				ValidateFunc: validation.StringInSlice([]string{
					ec2.TenancyDedicated,
					ec2.TenancyDefault,
					ec2.TenancyHost,
				}, false),
			},
			"user_data": {
				Type:          schema.TypeString,
				Optional:      true,
				Computed:      true,
				ConflictsWith: []string{"user_data_base64"},
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					// Sometimes the EC2 API responds with the equivalent, empty SHA1 sum
					// echo -n "" | shasum
					if (old == "da39a3ee5e6b4b0d3255bfef95601890afd80709" && new == "") ||
						(old == "" && new == "da39a3ee5e6b4b0d3255bfef95601890afd80709") {
						return true
					}
					return false
				},
				StateFunc: func(v interface{}) string {
					switch v := v.(type) {
					case string:
						return userDataHashSum(v)
					default:
						return ""
					}
				},
				ValidateFunc: validation.StringLenBetween(0, 16384),
			},
			"user_data_base64": {
				Type:          schema.TypeString,
				Optional:      true,
				Computed:      true,
				ConflictsWith: []string{"user_data"},
				ValidateFunc: func(v interface{}, name string) (warns []string, errs []error) {
					s := v.(string)
					if !verify.IsBase64Encoded([]byte(s)) {
						errs = append(errs, fmt.Errorf(
							"%s: must be base64-encoded", name,
						))
					}
					return
				},
			},
			"volume_tags": tftags.TagsSchema(),
			"vpc_security_group_ids": {
				Type:     schema.TypeSet,
				Optional: true,
				Computed: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Set:      schema.HashString,
			},

			"capacity_reservation_specification": {
				Type:     schema.TypeList,
				MaxItems: 1,
				Optional: true,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"capacity_reservation_preference": {
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validation.StringInSlice(ec2.CapacityReservationPreference_Values(), false),
							ExactlyOneOf: []string{"capacity_reservation_specification.0.capacity_reservation_preference", "capacity_reservation_specification.0.capacity_reservation_target"},
						},
						"capacity_reservation_target": {
							Type:     schema.TypeList,
							MaxItems: 1,
							Optional: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"capacity_reservation_id": {
										Type:     schema.TypeString,
										Optional: true,
									},
								},
							},
							ExactlyOneOf: []string{"capacity_reservation_specification.0.capacity_reservation_preference", "capacity_reservation_specification.0.capacity_reservation_target"},
						},
					},
				},
			},
		},

		CustomizeDiff: customdiff.All(
			verify.SetTagsDiff,
			func(_ context.Context, diff *schema.ResourceDiff, meta interface{}) error {
				_, ok := diff.GetOk("launch_template")

				if diff.Id() != "" && diff.HasChange("launch_template.0.version") && ok {
					conn := meta.(*conns.AWSClient).EC2Conn

					stateVersion := diff.Get("launch_template.0.version")

					var err error
					var templateId, instanceVersion, defaultVersion, latestVersion string

					templateId, err = getInstanceLaunchTemplateID(conn, diff.Id())
					if err != nil {
						return err
					}

					if templateId != "" {
						instanceVersion, err = getInstanceLaunchTemplateVersion(conn, diff.Id())
						if err != nil {
							return err
						}

						_, defaultVersion, latestVersion, err = getLaunchTemplateSpecification(conn, templateId)
						if err != nil {
							return err
						}
					}

					switch stateVersion {
					case "$Default":
						if instanceVersion != defaultVersion {
							diff.ForceNew("launch_template.0.version")
						}
					case "$Latest":
						if instanceVersion != latestVersion {
							diff.ForceNew("launch_template.0.version")
						}
					default:
						if stateVersion != instanceVersion {
							diff.ForceNew("launch_template.0.version")
						}
					}
				}

				return nil
			},
			customdiff.ComputedIf("launch_template.0.id", func(_ context.Context, diff *schema.ResourceDiff, meta interface{}) bool {
				return diff.HasChange("launch_template.0.name")
			}),
			customdiff.ComputedIf("launch_template.0.name", func(_ context.Context, diff *schema.ResourceDiff, meta interface{}) bool {
				return diff.HasChange("launch_template.0.id")
			}),
		),
	}
}

func iopsDiffSuppressFunc(k, old, new string, d *schema.ResourceData) bool {
	// Suppress diff if volume_type is not io1, io2, or gp3 and iops is unset or configured as 0
	i := strings.LastIndexByte(k, '.')
	vt := k[:i+1] + "volume_type"
	v := d.Get(vt).(string)
	return (strings.ToLower(v) != ec2.VolumeTypeIo1 && strings.ToLower(v) != ec2.VolumeTypeIo2 && strings.ToLower(v) != ec2.VolumeTypeGp3) && new == "0"
}

func throughputDiffSuppressFunc(k, old, new string, d *schema.ResourceData) bool {
	// Suppress diff if volume_type is not gp3 and throughput is unset or configured as 0
	i := strings.LastIndexByte(k, '.')
	vt := k[:i+1] + "volume_type"
	v := d.Get(vt).(string)
	return strings.ToLower(v) != ec2.VolumeTypeGp3 && new == "0"
}

func resourceInstanceCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*conns.AWSClient).EC2Conn
	defaultTagsConfig := meta.(*conns.AWSClient).DefaultTagsConfig
	tags := defaultTagsConfig.MergeTags(tftags.New(d.Get("tags").(map[string]interface{})))

	instanceOpts, err := buildInstanceOpts(d, meta)
	if err != nil {
		return fmt.Errorf("error collecting instance settings: %w", err)
	}

	tagSpecifications := ec2TagSpecificationsFromKeyValueTags(tags, ec2.ResourceTypeInstance)
	tagSpecifications = append(tagSpecifications, ec2TagSpecificationsFromMap(d.Get("volume_tags").(map[string]interface{}), ec2.ResourceTypeVolume)...)

	// Build the creation struct
	runOpts := &ec2.RunInstancesInput{
		BlockDeviceMappings:               instanceOpts.BlockDeviceMappings,
		CapacityReservationSpecification:  instanceOpts.CapacityReservationSpecification,
		DisableApiTermination:             instanceOpts.DisableAPITermination,
		EbsOptimized:                      instanceOpts.EBSOptimized,
		Monitoring:                        instanceOpts.Monitoring,
		IamInstanceProfile:                instanceOpts.IAMInstanceProfile,
		ImageId:                           instanceOpts.ImageID,
		InstanceInitiatedShutdownBehavior: instanceOpts.InstanceInitiatedShutdownBehavior,
		InstanceType:                      instanceOpts.InstanceType,
		Ipv6AddressCount:                  instanceOpts.Ipv6AddressCount,
		Ipv6Addresses:                     instanceOpts.Ipv6Addresses,
		KeyName:                           instanceOpts.KeyName,
		LaunchTemplate:                    instanceOpts.LaunchTemplate,
		MaxCount:                          aws.Int64(1),
		MinCount:                          aws.Int64(1),
		NetworkInterfaces:                 instanceOpts.NetworkInterfaces,
		Placement:                         instanceOpts.Placement,
		PrivateIpAddress:                  instanceOpts.PrivateIPAddress,
		SecurityGroupIds:                  instanceOpts.SecurityGroupIDs,
		SecurityGroups:                    instanceOpts.SecurityGroups,
		SubnetId:                          instanceOpts.SubnetID,
		UserData:                          instanceOpts.UserData64,
		CreditSpecification:               instanceOpts.CreditSpecification,
		CpuOptions:                        instanceOpts.CpuOptions,
		HibernationOptions:                instanceOpts.HibernationOptions,
		MetadataOptions:                   instanceOpts.MetadataOptions,
		EnclaveOptions:                    instanceOpts.EnclaveOptions,
		TagSpecifications:                 tagSpecifications,
	}

	_, ipv6CountOk := d.GetOk("ipv6_address_count")
	_, ipv6AddressOk := d.GetOk("ipv6_addresses")

	if ipv6AddressOk && ipv6CountOk {
		return fmt.Errorf("Only 1 of `ipv6_address_count` or `ipv6_addresses` can be specified")
	}

	// Create the instance
	log.Printf("[DEBUG] Run configuration: %s", runOpts)

	var runResp *ec2.Reservation
	err = resource.Retry(tfiam.PropagationTimeout, func() *resource.RetryError {
		var err error
		runResp, err = conn.RunInstances(runOpts)
		// IAM instance profiles can take ~10 seconds to propagate in AWS:
		// http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/iam-roles-for-amazon-ec2.html#launch-instance-with-role-console
		if tfawserr.ErrMessageContains(err, "InvalidParameterValue", "Invalid IAM Instance Profile") {
			log.Print("[DEBUG] Invalid IAM Instance Profile referenced, retrying...")
			return resource.RetryableError(err)
		}
		// IAM roles can also take time to propagate in AWS:
		if tfawserr.ErrMessageContains(err, "InvalidParameterValue", " has no associated IAM Roles") {
			log.Print("[DEBUG] IAM Instance Profile appears to have no IAM roles, retrying...")
			return resource.RetryableError(err)
		}
		if err != nil {
			return resource.NonRetryableError(err)
		}
		return nil
	})
	if tfresource.TimedOut(err) {
		runResp, err = conn.RunInstances(runOpts)
	}
	// Warn if the AWS Error involves group ids, to help identify situation
	// where a user uses group ids in security_groups for the Default VPC.
	//   See https://github.com/hashicorp/terraform/issues/3798
	if tfawserr.ErrMessageContains(err, "InvalidParameterValue", "groupId is invalid") {
		return fmt.Errorf("Error launching instance, possible mismatch of Security Group IDs and Names. See AWS Instance docs here: %s.\n\n\tAWS Error: %w", "https://terraform.io/docs/providers/aws/r/instance.html", err)
	}
	if err != nil {
		return fmt.Errorf("Error launching source instance: %s", err)
	}
	if runResp == nil || len(runResp.Instances) == 0 {
		return errors.New("Error launching source instance: no instances returned in response")
	}

	instance := runResp.Instances[0]
	log.Printf("[INFO] Instance ID: %s", aws.StringValue(instance.InstanceId))

	// Store the resulting ID so we can look this up later
	d.SetId(aws.StringValue(instance.InstanceId))

	// Wait for the instance to become running so we can get some attributes
	// that aren't available until later.
	log.Printf(
		"[DEBUG] Waiting for instance (%s) to become running",
		aws.StringValue(instance.InstanceId))

	stateConf := &resource.StateChangeConf{
		Pending:    []string{ec2.InstanceStateNamePending},
		Target:     []string{ec2.InstanceStateNameRunning},
		Refresh:    InstanceStateRefreshFunc(conn, aws.StringValue(instance.InstanceId), []string{ec2.InstanceStateNameTerminated, ec2.InstanceStateNameShuttingDown}),
		Timeout:    d.Timeout(schema.TimeoutCreate),
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	instanceRaw, err := stateConf.WaitForState()
	if err != nil {
		return fmt.Errorf(
			"Error waiting for instance (%s) to become ready: %s",
			aws.StringValue(instance.InstanceId), err)
	}

	instance = instanceRaw.(*ec2.Instance)

	// Initialize the connection info
	if instance.PublicIpAddress != nil {
		d.SetConnInfo(map[string]string{
			"type": "ssh",
			"host": aws.StringValue(instance.PublicIpAddress),
		})
	} else if instance.PrivateIpAddress != nil {
		d.SetConnInfo(map[string]string{
			"type": "ssh",
			"host": aws.StringValue(instance.PrivateIpAddress),
		})
	}

	// tags in root_block_device and ebs_block_device
	blockDeviceTagsToCreate := map[string]map[string]interface{}{}
	if v, ok := d.GetOk("root_block_device"); ok {
		vL := v.([]interface{})
		for _, v := range vL {
			bd := v.(map[string]interface{})
			if blockDeviceTags, ok := bd["tags"].(map[string]interface{}); ok && len(blockDeviceTags) > 0 {
				if rootVolumeId := getRootVolumeId(instance); rootVolumeId != "" {
					blockDeviceTagsToCreate[rootVolumeId] = blockDeviceTags
				}
			}
		}
	}

	if v, ok := d.GetOk("ebs_block_device"); ok {
		vL := v.(*schema.Set).List()
		for _, v := range vL {
			bd := v.(map[string]interface{})
			if blockDeviceTags, ok := bd["tags"].(map[string]interface{}); ok && len(blockDeviceTags) > 0 {
				devName := bd["device_name"].(string)
				if volumeId := getVolumeIdByDeviceName(instance, devName); volumeId != "" {
					blockDeviceTagsToCreate[volumeId] = blockDeviceTags
				}
			}
		}
	}

	for vol, blockDeviceTags := range blockDeviceTagsToCreate {
		if err := CreateTags(conn, vol, blockDeviceTags); err != nil {
			log.Printf("[ERR] Error creating tags for EBS volume %s: %s", vol, err)
		}
	}

	// Update if we need to
	return resourceInstanceUpdate(d, meta)
}

func resourceInstanceRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*conns.AWSClient).EC2Conn
	defaultTagsConfig := meta.(*conns.AWSClient).DefaultTagsConfig
	ignoreTagsConfig := meta.(*conns.AWSClient).IgnoreTagsConfig

	instance, err := InstanceFindByID(conn, d.Id())
	if err != nil {
		// If the instance was not found, return nil so that we can show
		// that the instance is gone.
		if tfawserr.ErrMessageContains(err, "InvalidInstanceID.NotFound", "") {
			log.Printf("[WARN] EC2 Instance (%s) not found, removing from state", d.Id())
			d.SetId("")
			return nil
		}

		// Some other error, report it
		return fmt.Errorf("error retrieving instance (%s): %w", d.Id(), err)
	}

	// If nothing was found, then return no state
	if instance == nil {
		log.Printf("[WARN] EC2 Instance (%s) not found, removing from state", d.Id())
		d.SetId("")
		return nil
	}

	if instance.State != nil {
		// If the instance is terminated, then it is gone
		if aws.StringValue(instance.State.Name) == ec2.InstanceStateNameTerminated {
			d.SetId("")
			return nil
		}

		d.Set("instance_state", instance.State.Name)
	}

	if instance.Placement != nil {
		d.Set("availability_zone", instance.Placement.AvailabilityZone)
	}
	if instance.Placement.GroupName != nil {
		d.Set("placement_group", instance.Placement.GroupName)
	}
	if instance.Placement.PartitionNumber != nil {
		d.Set("placement_partition_number", instance.Placement.PartitionNumber)
	}
	if instance.Placement.Tenancy != nil {
		d.Set("tenancy", instance.Placement.Tenancy)
	}
	if instance.Placement.HostId != nil {
		d.Set("host_id", instance.Placement.HostId)
	}

	if instance.CpuOptions != nil {
		d.Set("cpu_core_count", instance.CpuOptions.CoreCount)
		d.Set("cpu_threads_per_core", instance.CpuOptions.ThreadsPerCore)
	}

	if instance.HibernationOptions != nil {
		d.Set("hibernation", instance.HibernationOptions.Configured)
	}

	if err := d.Set("metadata_options", flattenEc2InstanceMetadataOptions(instance.MetadataOptions)); err != nil {
		return fmt.Errorf("error setting metadata_options: %s", err)
	}

	if err := d.Set("enclave_options", flattenEc2EnclaveOptions(instance.EnclaveOptions)); err != nil {
		return fmt.Errorf("error setting enclave_options: %s", err)
	}

	d.Set("ami", instance.ImageId)
	d.Set("instance_type", instance.InstanceType)
	d.Set("key_name", instance.KeyName)
	d.Set("public_dns", instance.PublicDnsName)
	d.Set("public_ip", instance.PublicIpAddress)
	d.Set("private_dns", instance.PrivateDnsName)
	d.Set("private_ip", instance.PrivateIpAddress)
	d.Set("outpost_arn", instance.OutpostArn)

	if instance.IamInstanceProfile != nil && instance.IamInstanceProfile.Arn != nil {
		name, err := tfiam.InstanceProfileARNToName(aws.StringValue(instance.IamInstanceProfile.Arn))

		if err != nil {
			return fmt.Errorf("error setting iam_instance_profile: %w", err)
		}

		d.Set("iam_instance_profile", name)
	} else {
		d.Set("iam_instance_profile", nil)
	}

	{
		launchTemplate, err := getInstanceLaunchTemplate(conn, d)
		if err != nil {
			return fmt.Errorf("error reading Instance (%s) Launch Template: %w", d.Id(), err)
		}
		if err := d.Set("launch_template", launchTemplate); err != nil {
			return fmt.Errorf("error setting launch_template: %w", err)
		}
	}

	// Set configured Network Interface Device Index Slice
	// We only want to read, and populate state for the configured network_interface attachments. Otherwise, other
	// resources have the potential to attach network interfaces to the instance, and cause a perpetual create/destroy
	// diff. We should only read on changes configured for this specific resource because of this.
	var configuredDeviceIndexes []int
	if v, ok := d.GetOk("network_interface"); ok {
		vL := v.(*schema.Set).List()
		for _, vi := range vL {
			mVi := vi.(map[string]interface{})
			configuredDeviceIndexes = append(configuredDeviceIndexes, mVi["device_index"].(int))
		}
	}

	var secondaryPrivateIPs []string
	var ipv6Addresses []string
	if len(instance.NetworkInterfaces) > 0 {
		var primaryNetworkInterface ec2.InstanceNetworkInterface
		var networkInterfaces []map[string]interface{}
		for _, iNi := range instance.NetworkInterfaces {
			ni := make(map[string]interface{})
			if aws.Int64Value(iNi.Attachment.DeviceIndex) == 0 {
				primaryNetworkInterface = *iNi
			}
			// If the attached network device is inside our configuration, refresh state with values found.
			// Otherwise, assume the network device was attached via an outside resource.
			for _, index := range configuredDeviceIndexes {
				if index == int(aws.Int64Value(iNi.Attachment.DeviceIndex)) {
					ni["device_index"] = aws.Int64Value(iNi.Attachment.DeviceIndex)
					ni["network_interface_id"] = aws.StringValue(iNi.NetworkInterfaceId)
					ni["delete_on_termination"] = aws.BoolValue(iNi.Attachment.DeleteOnTermination)
				}
			}
			// Don't add empty network interfaces to schema
			if len(ni) == 0 {
				continue
			}
			networkInterfaces = append(networkInterfaces, ni)
		}
		if err := d.Set("network_interface", networkInterfaces); err != nil {
			return fmt.Errorf("Error setting network_interfaces: %v", err)
		}

		// Set primary network interface details
		// If an instance is shutting down, network interfaces are detached, and attributes may be nil,
		// need to protect against nil pointer dereferences
		if primaryNetworkInterface.SubnetId != nil {
			d.Set("subnet_id", primaryNetworkInterface.SubnetId)
		}
		if primaryNetworkInterface.NetworkInterfaceId != nil {
			d.Set("primary_network_interface_id", primaryNetworkInterface.NetworkInterfaceId)
		}
		d.Set("ipv6_address_count", len(primaryNetworkInterface.Ipv6Addresses))
		if primaryNetworkInterface.SourceDestCheck != nil {
			d.Set("source_dest_check", primaryNetworkInterface.SourceDestCheck)
		}

		d.Set("associate_public_ip_address", primaryNetworkInterface.Association != nil)

		for _, address := range primaryNetworkInterface.PrivateIpAddresses {
			if !aws.BoolValue(address.Primary) {
				secondaryPrivateIPs = append(secondaryPrivateIPs, aws.StringValue(address.PrivateIpAddress))
			}
		}

		for _, address := range primaryNetworkInterface.Ipv6Addresses {
			ipv6Addresses = append(ipv6Addresses, aws.StringValue(address.Ipv6Address))
		}

	} else {
		d.Set("associate_public_ip_address", instance.PublicIpAddress != nil)
		d.Set("ipv6_address_count", 0)
		d.Set("primary_network_interface_id", "")
		d.Set("subnet_id", instance.SubnetId)
	}

	if err := d.Set("secondary_private_ips", secondaryPrivateIPs); err != nil {
		return fmt.Errorf("Error setting private_ips for AWS Instance (%s): %w", d.Id(), err)
	}

	if err := d.Set("ipv6_addresses", ipv6Addresses); err != nil {
		log.Printf("[WARN] Error setting ipv6_addresses for AWS Instance (%s): %s", d.Id(), err)
	}

	d.Set("ebs_optimized", instance.EbsOptimized)
	if aws.StringValue(instance.SubnetId) != "" {
		d.Set("source_dest_check", instance.SourceDestCheck)
	}

	if instance.Monitoring != nil && instance.Monitoring.State != nil {
		monitoringState := aws.StringValue(instance.Monitoring.State)
		d.Set("monitoring", monitoringState == ec2.MonitoringStateEnabled || monitoringState == ec2.MonitoringStatePending)
	}

	tags := KeyValueTags(instance.Tags).IgnoreAWS().IgnoreConfig(ignoreTagsConfig)

	//lintignore:AWSR002
	if err := d.Set("tags", tags.RemoveDefaultConfig(defaultTagsConfig).Map()); err != nil {
		return fmt.Errorf("error setting tags: %w", err)
	}

	if err := d.Set("tags_all", tags.Map()); err != nil {
		return fmt.Errorf("error setting tags_all: %w", err)
	}

	if _, ok := d.GetOk("volume_tags"); ok && !blockDeviceTagsDefined(d) {
		volumeTags, err := readVolumeTags(conn, d.Id())
		if err != nil {
			return err
		}

		if err := d.Set("volume_tags", KeyValueTags(volumeTags).IgnoreAWS().Map()); err != nil {
			return fmt.Errorf("error setting volume_tags: %s", err)
		}
	}

	if err := readSecurityGroups(d, instance, conn); err != nil {
		return err
	}

	// Retrieve instance shutdown behavior
	if err := readInstanceShutdownBehavior(d, conn); err != nil {
		return err
	}

	if err := readBlockDevices(d, instance, conn); err != nil {
		return err
	}
	if _, ok := d.GetOk("ephemeral_block_device"); !ok {
		d.Set("ephemeral_block_device", []interface{}{})
	}

	// ARN

	arn := arn.ARN{
		Partition: meta.(*conns.AWSClient).Partition,
		Region:    meta.(*conns.AWSClient).Region,
		Service:   ec2.ServiceName,
		AccountID: meta.(*conns.AWSClient).AccountID,
		Resource:  fmt.Sprintf("instance/%s", d.Id()),
	}
	d.Set("arn", arn.String())

	// Instance attributes
	{
		attr, err := conn.DescribeInstanceAttribute(&ec2.DescribeInstanceAttributeInput{
			Attribute:  aws.String("disableApiTermination"),
			InstanceId: aws.String(d.Id()),
		})
		if err != nil {
			return err
		}
		d.Set("disable_api_termination", attr.DisableApiTermination.Value)
	}
	{
		attr, err := conn.DescribeInstanceAttribute(&ec2.DescribeInstanceAttributeInput{
			Attribute:  aws.String(ec2.InstanceAttributeNameUserData),
			InstanceId: aws.String(d.Id()),
		})
		if err != nil {
			return err
		}
		if attr.UserData != nil && attr.UserData.Value != nil {
			// Since user_data and user_data_base64 conflict with each other,
			// we'll only set one or the other here to avoid a perma-diff.
			// Since user_data_base64 was added later, we'll prefer to set
			// user_data.
			_, b64 := d.GetOk("user_data_base64")
			if b64 {
				d.Set("user_data_base64", attr.UserData.Value)
			} else {
				d.Set("user_data", userDataHashSum(aws.StringValue(attr.UserData.Value)))
			}
		}
	}

	// AWS Standard will return InstanceCreditSpecification.NotSupported errors for EC2 Instance IDs outside T2 and T3 instance types
	// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/8055
	if strings.HasPrefix(aws.StringValue(instance.InstanceType), "t2") || strings.HasPrefix(aws.StringValue(instance.InstanceType), "t3") {
		creditSpecifications, err := getCreditSpecifications(conn, d.Id())

		// Ignore UnsupportedOperation errors for AWS China and GovCloud (US)
		// Reference: https://github.com/hashicorp/terraform-provider-aws/pull/4362
		if err != nil && !tfawserr.ErrMessageContains(err, "UnsupportedOperation", "") {
			return fmt.Errorf("error getting EC2 Instance (%s) Credit Specifications: %s", d.Id(), err)
		}

		if err := d.Set("credit_specification", creditSpecifications); err != nil {
			return fmt.Errorf("error setting credit_specification: %s", err)
		}
	}

	if d.Get("get_password_data").(bool) {
		passwordData, err := getInstancePasswordData(aws.StringValue(instance.InstanceId), conn)
		if err != nil {
			return err
		}
		d.Set("password_data", passwordData)
	} else {
		d.Set("get_password_data", false)
		d.Set("password_data", nil)
	}

	if err := d.Set("capacity_reservation_specification", flattenCapacityReservationSpecification(instance.CapacityReservationSpecification)); err != nil {
		return fmt.Errorf("error setting capacity reservation specification: %s", err)
	}

	return nil
}

func resourceInstanceUpdate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*conns.AWSClient).EC2Conn

	if d.HasChange("tags_all") && !d.IsNewResource() {
		o, n := d.GetChange("tags_all")

		if err := UpdateTags(conn, d.Id(), o, n); err != nil {
			return fmt.Errorf("error updating tags: %s", err)
		}
	}

	if d.HasChange("volume_tags") && !d.IsNewResource() {
		volumeIds, err := getInstanceVolumeIDs(conn, d.Id())
		if err != nil {
			return err
		}

		o, n := d.GetChange("volume_tags")

		for _, volumeId := range volumeIds {
			if err := UpdateTags(conn, volumeId, o, n); err != nil {
				return fmt.Errorf("error updating volume_tags (%s): %s", volumeId, err)
			}
		}
	}

	if d.HasChange("iam_instance_profile") && !d.IsNewResource() {
		request := &ec2.DescribeIamInstanceProfileAssociationsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("instance-id"),
					Values: []*string{aws.String(d.Id())},
				},
			},
		}

		resp, err := conn.DescribeIamInstanceProfileAssociations(request)
		if err != nil {
			return err
		}

		// An Iam Instance Profile has been provided and is pending a change
		// This means it is an association or a replacement to an association
		if _, ok := d.GetOk("iam_instance_profile"); ok {
			// Does not have an Iam Instance Profile associated with it, need to associate
			if len(resp.IamInstanceProfileAssociations) == 0 {
				if err := associateInstanceProfile(d, conn); err != nil {
					return err
				}
			} else {
				// Has an Iam Instance Profile associated with it, need to replace the association
				associationId := resp.IamInstanceProfileAssociations[0].AssociationId
				input := &ec2.ReplaceIamInstanceProfileAssociationInput{
					AssociationId: associationId,
					IamInstanceProfile: &ec2.IamInstanceProfileSpecification{
						Name: aws.String(d.Get("iam_instance_profile").(string)),
					},
				}

				// If the instance is running, we can replace the instance profile association.
				// If it is stopped, the association must be removed and the new one attached separately. (GH-8262)
				instanceState := d.Get("instance_state").(string)

				if instanceState != "" {
					if instanceState == ec2.InstanceStateNameStopped || instanceState == ec2.InstanceStateNameStopping || instanceState == ec2.InstanceStateNameShuttingDown {
						if err := disassociateInstanceProfile(associationId, conn); err != nil {
							return err
						}
						if err := associateInstanceProfile(d, conn); err != nil {
							return err
						}
					} else {
						err := resource.Retry(tfiam.PropagationTimeout, func() *resource.RetryError {
							_, err := conn.ReplaceIamInstanceProfileAssociation(input)
							if err != nil {
								if tfawserr.ErrMessageContains(err, "InvalidParameterValue", "Invalid IAM Instance Profile") {
									return resource.RetryableError(err)
								}
								return resource.NonRetryableError(err)
							}
							return nil
						})
						if tfresource.TimedOut(err) {
							_, err = conn.ReplaceIamInstanceProfileAssociation(input)
						}
						if err != nil {
							return fmt.Errorf("Error replacing instance profile association: %s", err)
						}
					}
				}
			}
			// An Iam Instance Profile has _not_ been provided but is pending a change. This means there is a pending removal
		} else {
			if len(resp.IamInstanceProfileAssociations) > 0 {
				// Has an Iam Instance Profile associated with it, need to remove the association
				associationId := resp.IamInstanceProfileAssociations[0].AssociationId
				if err := disassociateInstanceProfile(associationId, conn); err != nil {
					return err
				}
			}
		}

		if _, err := WaitInstanceIAMInstanceProfileUpdated(conn, d.Id(), d.Get("iam_instance_profile").(string)); err != nil {
			return fmt.Errorf("error waiting for EC2 Instance (%s) IAM Instance Profile update: %w", d.Id(), err)
		}
	}

	// SourceDestCheck can only be modified on an instance without manually specified network interfaces.
	// SourceDestCheck, in that case, is configured at the network interface level
	if _, ok := d.GetOk("network_interface"); !ok {

		// If we have a new resource and source_dest_check is still true, don't modify
		sourceDestCheck := d.Get("source_dest_check").(bool)

		// Because we're calling Update prior to Read, and the default value of `source_dest_check` is `true`,
		// HasChange() thinks there is a diff between what is set on the instance and what is set in state. We need to ensure that
		// if a diff has occurred, it's not because it's a new instance.
		if d.HasChange("source_dest_check") && !d.IsNewResource() || d.IsNewResource() && !sourceDestCheck {
			// SourceDestCheck can only be set on VPC instances
			// AWS will return an error of InvalidParameterCombination if we attempt
			// to modify the source_dest_check of an instance in EC2 Classic
			log.Printf("[INFO] Modifying `source_dest_check` on Instance %s", d.Id())
			_, err := conn.ModifyInstanceAttribute(&ec2.ModifyInstanceAttributeInput{
				InstanceId: aws.String(d.Id()),
				SourceDestCheck: &ec2.AttributeBooleanValue{
					Value: aws.Bool(sourceDestCheck),
				},
			})
			if err != nil {
				// Tolerate InvalidParameterCombination error in Classic, otherwise
				// return the error
				if !tfawserr.ErrMessageContains(err, "InvalidParameterCombination", "") {
					return err
				}
				log.Printf("[WARN] Attempted to modify SourceDestCheck on non VPC instance: %s", err)
			}
		}
	}

	if d.HasChanges("secondary_private_ips", "vpc_security_group_ids") && !d.IsNewResource() {
		instance, err := InstanceFindByID(conn, d.Id())
		if err != nil {
			return fmt.Errorf("error retrieving instance %q: %w", d.Id(), err)
		}
		var primaryInterface ec2.InstanceNetworkInterface
		for _, ni := range instance.NetworkInterfaces {
			if aws.Int64Value(ni.Attachment.DeviceIndex) == 0 {
				primaryInterface = *ni
			}
		}

		if d.HasChange("secondary_private_ips") {
			if primaryInterface.NetworkInterfaceId == nil {
				return fmt.Errorf("Failed to update secondary_private_ips on %q, which does not contain a primary network interface",
					d.Id())
			}
			o, n := d.GetChange("secondary_private_ips")
			if o == nil {
				o = new(schema.Set)
			}
			if n == nil {
				n = new(schema.Set)
			}

			os := o.(*schema.Set)
			ns := n.(*schema.Set)

			// Unassign old IP addresses
			unassignIps := os.Difference(ns)
			if unassignIps.Len() != 0 {
				input := &ec2.UnassignPrivateIpAddressesInput{
					NetworkInterfaceId: primaryInterface.NetworkInterfaceId,
					PrivateIpAddresses: flex.ExpandStringSet(unassignIps),
				}
				log.Printf("[INFO] Unassigning secondary_private_ips on Instance %q", d.Id())
				_, err := conn.UnassignPrivateIpAddresses(input)
				if err != nil {
					return fmt.Errorf("Failure to unassign Secondary Private IPs: %w", err)
				}
			}

			// Assign new IP addresses
			assignIps := ns.Difference(os)
			if assignIps.Len() != 0 {
				input := &ec2.AssignPrivateIpAddressesInput{
					NetworkInterfaceId: primaryInterface.NetworkInterfaceId,
					PrivateIpAddresses: flex.ExpandStringSet(assignIps),
				}
				log.Printf("[INFO] Assigning secondary_private_ips on Instance %q", d.Id())
				_, err := conn.AssignPrivateIpAddresses(input)
				if err != nil {
					return fmt.Errorf("Failure to assign Secondary Private IPs: %w", err)
				}
			}
		}

		if d.HasChange("vpc_security_group_ids") {
			if primaryInterface.NetworkInterfaceId == nil {
				return fmt.Errorf("Failed to update vpc_security_group_ids on %q, which does not contain a primary network interface",
					d.Id())
			}
			var groups []*string
			if v := d.Get("vpc_security_group_ids").(*schema.Set); v.Len() > 0 {
				for _, v := range v.List() {
					groups = append(groups, aws.String(v.(string)))
				}
			}

			if len(groups) < 1 {
				return fmt.Errorf("VPC-based instances require at least one security group to be attached.")
			}
			// If a user has multiple network interface attachments on the target EC2 instance, simply modifying the
			// instance attributes via a `ModifyInstanceAttributes()` request would fail with the following error message:
			// "There are multiple interfaces attached to instance 'i-XX'. Please specify an interface ID for the operation instead."
			// Thus, we need to actually modify the primary network interface for the new security groups, as the primary
			// network interface is where we modify/create security group assignments during Create.
			log.Printf("[INFO] Modifying `vpc_security_group_ids` on Instance %q", d.Id())
			if _, err := conn.ModifyNetworkInterfaceAttribute(&ec2.ModifyNetworkInterfaceAttributeInput{
				NetworkInterfaceId: primaryInterface.NetworkInterfaceId,
				Groups:             groups,
			}); err != nil {
				return err
			}
		}
	}

	if d.HasChanges("instance_type", "user_data", "user_data_base64") && !d.IsNewResource() {
		// For each argument change, we start and stop the instance
		// to account for behaviors occurring outside terraform.
		// Only one attribute can be modified at a time, else we get
		// "InvalidParameterCombination: Fields for multiple attribute types specified"
		if d.HasChange("instance_type") {
			log.Printf("[INFO] Modifying instance type %s", d.Id())

			input := &ec2.ModifyInstanceAttributeInput{
				InstanceId: aws.String(d.Id()),
				InstanceType: &ec2.AttributeValue{
					Value: aws.String(d.Get("instance_type").(string)),
				},
			}

			if err := modifyAttributeWithInstanceStopStart(d, conn, input); err != nil {
				return fmt.Errorf("error updating instance (%s) instance type: %w", d.Id(), err)
			}
		}

		// From the API reference:
		// "If you are using an AWS SDK or command line tool,
		// base64-encoding is performed for you, and you can load the text from a file.
		// Otherwise, you must provide base64-encoded text".

		if d.HasChange("user_data") {
			log.Printf("[INFO] Modifying user data %s", d.Id())

			input := &ec2.ModifyInstanceAttributeInput{
				InstanceId: aws.String(d.Id()),
			}

			userDataStr := d.Get("user_data").(string)

			if verify.IsBase64Encoded([]byte(userDataStr)) {
				// Decode so the AWS SDK doesn't double encode
				userData, err := base64.StdEncoding.DecodeString(userDataStr)
				if err != nil {
					return fmt.Errorf("error decoding instance (%s) user data during update: %w", d.Id(), err)
				}

				input.UserData = &ec2.BlobAttributeValue{
					Value: userData,
				}
			} else {
				input.UserData = &ec2.BlobAttributeValue{
					Value: []byte(userDataStr),
				}
			}

			if err := modifyAttributeWithInstanceStopStart(d, conn, input); err != nil {
				return fmt.Errorf("error updating instance (%s) user data: %w", d.Id(), err)
			}
		}

		if d.HasChange("user_data_base64") {
			log.Printf("[INFO] Modifying user data base64 %s", d.Id())

			input := &ec2.ModifyInstanceAttributeInput{
				InstanceId: aws.String(d.Id()),
			}

			userDataBase64Str := d.Get("user_data_base64").(string)

			// Schema validation technically ensures the data is Base64 encoded
			if verify.IsBase64Encoded([]byte(userDataBase64Str)) {
				// Decode so the AWS SDK doesn't double encode
				userDataBase64, err := base64.StdEncoding.DecodeString(userDataBase64Str)
				if err != nil {
					return fmt.Errorf("error decoding instance (%s) user data base64 during update: %w", d.Id(), err)
				}

				input.UserData = &ec2.BlobAttributeValue{
					Value: userDataBase64,
				}
			} else {
				input.UserData = &ec2.BlobAttributeValue{
					Value: []byte(userDataBase64Str),
				}
			}

			if err := modifyAttributeWithInstanceStopStart(d, conn, input); err != nil {
				return fmt.Errorf("error updating instance (%s) user data base64: %w", d.Id(), err)
			}
		}
	}

	if d.HasChange("disable_api_termination") && !d.IsNewResource() {
		err := resourceInstanceDisableAPITermination(conn, d.Id(), d.Get("disable_api_termination").(bool))

		if err != nil {
			return fmt.Errorf("error modifying instance (%s) attribute (%s): %w", d.Id(), ec2.InstanceAttributeNameDisableApiTermination, err)
		}
	}

	if d.HasChange("instance_initiated_shutdown_behavior") {
		log.Printf("[INFO] Modifying instance %s", d.Id())
		_, err := conn.ModifyInstanceAttribute(&ec2.ModifyInstanceAttributeInput{
			InstanceId: aws.String(d.Id()),
			InstanceInitiatedShutdownBehavior: &ec2.AttributeValue{
				Value: aws.String(d.Get("instance_initiated_shutdown_behavior").(string)),
			},
		})
		if err != nil {
			return err
		}
	}

	if d.HasChange("monitoring") {
		var mErr error
		if d.Get("monitoring").(bool) {
			log.Printf("[DEBUG] Enabling monitoring for Instance (%s)", d.Id())
			_, mErr = conn.MonitorInstances(&ec2.MonitorInstancesInput{
				InstanceIds: []*string{aws.String(d.Id())},
			})
		} else {
			log.Printf("[DEBUG] Disabling monitoring for Instance (%s)", d.Id())
			_, mErr = conn.UnmonitorInstances(&ec2.UnmonitorInstancesInput{
				InstanceIds: []*string{aws.String(d.Id())},
			})
		}
		if mErr != nil {
			return fmt.Errorf("Error updating Instance monitoring: %s", mErr)
		}
	}

	if d.HasChange("credit_specification") && !d.IsNewResource() {
		if v, ok := d.GetOk("credit_specification"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
			creditSpecification := v.([]interface{})[0].(map[string]interface{})
			log.Printf("[DEBUG] Modifying credit specification for Instance (%s)", d.Id())
			_, err := conn.ModifyInstanceCreditSpecification(&ec2.ModifyInstanceCreditSpecificationInput{
				InstanceCreditSpecifications: []*ec2.InstanceCreditSpecificationRequest{
					{
						InstanceId: aws.String(d.Id()),
						CpuCredits: aws.String(creditSpecification["cpu_credits"].(string)),
					},
				},
			})
			if err != nil {
				return fmt.Errorf("Error updating Instance credit specification: %s", err)
			}
		}
	}

	if d.HasChange("metadata_options") && !d.IsNewResource() {
		if v, ok := d.GetOk("metadata_options"); ok {
			if mo, ok := v.([]interface{})[0].(map[string]interface{}); ok {
				log.Printf("[DEBUG] Modifying metadata options for Instance (%s)", d.Id())
				input := &ec2.ModifyInstanceMetadataOptionsInput{
					InstanceId:   aws.String(d.Id()),
					HttpEndpoint: aws.String(mo["http_endpoint"].(string)),
				}
				if mo["http_endpoint"].(string) == ec2.InstanceMetadataEndpointStateEnabled {
					// These parameters are not allowed unless HttpEndpoint is enabled
					input.HttpTokens = aws.String(mo["http_tokens"].(string))
					input.HttpPutResponseHopLimit = aws.Int64(int64(mo["http_put_response_hop_limit"].(int)))
					input.InstanceMetadataTags = aws.String(mo["instance_metadata_tags"].(string))
				}
				_, err := conn.ModifyInstanceMetadataOptions(input)
				if err != nil {
					return fmt.Errorf("Error updating metadata options: %s", err)
				}

				stateConf := &resource.StateChangeConf{
					Pending:    []string{ec2.InstanceMetadataOptionsStatePending},
					Target:     []string{ec2.InstanceMetadataOptionsStateApplied},
					Refresh:    MetadataOptionsRefreshFunc(conn, d.Id()),
					Timeout:    d.Timeout(schema.TimeoutUpdate),
					Delay:      10 * time.Second,
					MinTimeout: 3 * time.Second,
				}

				_, err = stateConf.WaitForState()
				if err != nil {
					return fmt.Errorf(
						"Error waiting for instance (%s) to apply metadata options update: %s",
						d.Id(), err)
				}
			}
		}
	}

	if d.HasChange("root_block_device.0") && !d.IsNewResource() {
		volumeID := d.Get("root_block_device.0.volume_id").(string)

		input := ec2.ModifyVolumeInput{
			VolumeId: aws.String(volumeID),
		}
		modifyVolume := false

		if d.HasChange("root_block_device.0.volume_size") {
			if v, ok := d.Get("root_block_device.0.volume_size").(int); ok && v != 0 {
				modifyVolume = true
				input.Size = aws.Int64(int64(v))
			}
		}
		if d.HasChange("root_block_device.0.volume_type") {
			if v, ok := d.Get("root_block_device.0.volume_type").(string); ok && v != "" {
				modifyVolume = true
				input.VolumeType = aws.String(v)
			}
		}
		if d.HasChange("root_block_device.0.iops") {
			if v, ok := d.Get("root_block_device.0.iops").(int); ok && v != 0 {
				// Enforce IOPs usage with a valid volume type
				// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/12667
				if t, ok := d.Get("root_block_device.0.volume_type").(string); ok && t != ec2.VolumeTypeIo1 && t != ec2.VolumeTypeIo2 && t != ec2.VolumeTypeGp3 {
					if t == "" {
						// Volume defaults to gp2
						t = ec2.VolumeTypeGp2
					}
					return fmt.Errorf("error updating instance: iops attribute not supported for type %s", t)
				}
				modifyVolume = true
				input.Iops = aws.Int64(int64(v))
			}
		}
		if d.HasChange("root_block_device.0.throughput") {
			if v, ok := d.Get("root_block_device.0.throughput").(int); ok && v != 0 {
				// Enforce throughput usage with a valid volume type
				if t, ok := d.Get("root_block_device.0.volume_type").(string); ok && t != ec2.VolumeTypeGp3 {
					return fmt.Errorf("error updating instance: throughput attribute not supported for type %s", t)
				}
				modifyVolume = true
				input.Throughput = aws.Int64(int64(v))
			}
		}
		if modifyVolume {
			_, err := conn.ModifyVolume(&input)
			if err != nil {
				return fmt.Errorf("error modifying EC2 Volume %q: %w", volumeID, err)
			}

			// The volume is useable once the state is "optimizing", but will not be at full performance.
			// Optimization can take hours. e.g. a full 1 TiB drive takes approximately 6 hours to optimize,
			// according to https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-volume-modifications.html
			stateConf := &resource.StateChangeConf{
				Pending:    []string{ec2.VolumeModificationStateModifying},
				Target:     []string{ec2.VolumeModificationStateCompleted, ec2.VolumeModificationStateOptimizing},
				Refresh:    VolumeStateRefreshFunc(conn, volumeID, ec2.VolumeModificationStateFailed),
				Timeout:    d.Timeout(schema.TimeoutUpdate),
				Delay:      30 * time.Second,
				MinTimeout: 30 * time.Second,
			}

			_, err = stateConf.WaitForState()
			if err != nil {
				return fmt.Errorf("error waiting for EC2 volume (%s) to be modified: %w", volumeID, err)
			}
		}

		if d.HasChange("root_block_device.0.delete_on_termination") {
			deviceName := d.Get("root_block_device.0.device_name").(string)
			if v, ok := d.Get("root_block_device.0.delete_on_termination").(bool); ok {
				_, err := conn.ModifyInstanceAttribute(&ec2.ModifyInstanceAttributeInput{
					InstanceId: aws.String(d.Id()),
					BlockDeviceMappings: []*ec2.InstanceBlockDeviceMappingSpecification{
						{
							DeviceName: aws.String(deviceName),
							Ebs: &ec2.EbsInstanceBlockDeviceSpecification{
								DeleteOnTermination: aws.Bool(v),
							},
						},
					},
				})
				if err != nil {
					return fmt.Errorf("error modifying delete on termination attribute for EC2 instance %q block device %q: %w", d.Id(), deviceName, err)
				}

				stateConf := &resource.StateChangeConf{
					Target:     []string{strconv.FormatBool(v)},
					Refresh:    RootBlockDeviceDeleteOnTerminationRefreshFunc(conn, d.Id()),
					Timeout:    d.Timeout(schema.TimeoutUpdate),
					Delay:      10 * time.Second,
					MinTimeout: 3 * time.Second,
				}

				_, err = stateConf.WaitForState()
				if err != nil {
					return fmt.Errorf("Error waiting for instance (%s) to apply DeleteOnTermination attribute update: %s",
						d.Id(), err)
				}
			}
		}

		if d.HasChange("root_block_device.0.tags") {
			o, n := d.GetChange("root_block_device.0.tags")

			if err := UpdateTags(conn, volumeID, o, n); err != nil {
				return fmt.Errorf("error updating tags for volume (%s): %s", volumeID, err)
			}
		}
	}

	// To modify capacity reservation attributes of an instance, instance state needs to be in ec2.InstanceStateNameStopped,
	// otherwise the modification will return an IncorrectInstanceState error
	if d.HasChange("capacity_reservation_specification") && !d.IsNewResource() {
		if v, ok := d.GetOk("capacity_reservation_specification"); ok {
			capacityReservationSpecification := expandCapacityReservationSpecification(v.([]interface{}))
			if *capacityReservationSpecification != (ec2.CapacityReservationSpecification{}) && capacityReservationSpecification != nil {
				log.Printf("[DEBUG] Modifying capacity reservation for instance %s", d.Id())
				_, err := conn.ModifyInstanceCapacityReservationAttributes(&ec2.ModifyInstanceCapacityReservationAttributesInput{
					CapacityReservationSpecification: capacityReservationSpecification,
					InstanceId:                       aws.String(d.Id()),
				})

				if err != nil {
					return fmt.Errorf("Error updating instance capacity specification: %s", err)
				}
			}
		}
	}

	// TODO(mitchellh): wait for the attributes we modified to
	// persist the change...

	return resourceInstanceRead(d, meta)
}

func resourceInstanceDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*conns.AWSClient).EC2Conn

	err := resourceInstanceDisableAPITermination(conn, d.Id(), d.Get("disable_api_termination").(bool))

	if err != nil {
		log.Printf("[WARN] attempting to terminate EC2 instance (%s) despite error modifying attribute (%s): %s", d.Id(), ec2.InstanceAttributeNameDisableApiTermination, err)
	}

	err = terminateInstance(conn, d.Id(), d.Timeout(schema.TimeoutDelete))

	if err != nil {
		return fmt.Errorf("error terminating EC2 Instance (%s): %s", d.Id(), err)
	}

	return nil
}

func resourceInstanceDisableAPITermination(conn *ec2.EC2, id string, disableAPITermination bool) error {
	// false = enable api termination
	// true = disable api termination (protected)

	_, err := conn.ModifyInstanceAttribute(&ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(id),
		DisableApiTermination: &ec2.AttributeBooleanValue{
			Value: aws.Bool(disableAPITermination),
		},
	})

	if tfawserr.ErrMessageContains(err, "UnsupportedOperation", "not supported for spot instances") {
		log.Printf("[WARN] failed to modify instance (%s) attribute (%s): %s", id, ec2.InstanceAttributeNameDisableApiTermination, err)
		return nil
	}

	if err != nil {
		return fmt.Errorf("error modify instance (%s) attribute (%s) to value %t: %w", id, ec2.InstanceAttributeNameDisableApiTermination, disableAPITermination, err)
	}

	return nil
}

// modifyAttributeWithInstanceStopStart modifies a specific attribute provided
// as input by first stopping the EC2 instance before the modification
// and then starting up the EC2 instance after modification.
// Reference: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/Stop_Start.html
func modifyAttributeWithInstanceStopStart(d *schema.ResourceData, conn *ec2.EC2, input *ec2.ModifyInstanceAttributeInput) error {
	log.Printf("[INFO] Stopping Instance %q for attribute change", d.Id())
	_, err := conn.StopInstances(&ec2.StopInstancesInput{
		InstanceIds: []*string{aws.String(d.Id())},
	})

	if err != nil {
		return fmt.Errorf("error stopping EC2 Instance (%s): %w", d.Id(), err)
	}

	if err := WaitForInstanceStopping(conn, d.Id(), InstanceStopTimeout); err != nil {
		return err
	}

	if _, err := conn.ModifyInstanceAttribute(input); err != nil {
		return err
	}

	startInput := &ec2.StartInstancesInput{
		InstanceIds: []*string{aws.String(d.Id())},
	}

	// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/16433
	err = resource.Retry(InstanceAttributePropagationTimeout, func() *resource.RetryError {
		_, err := conn.StartInstances(startInput)

		if tfawserr.ErrMessageContains(err, ErrCodeInvalidParameterValue, "LaunchPlan instance type does not match attribute value") {
			return resource.RetryableError(err)
		}

		if err != nil {
			return resource.NonRetryableError(err)
		}

		return nil
	})

	if tfresource.TimedOut(err) {
		_, err = conn.StartInstances(startInput)
	}

	if err != nil {
		return fmt.Errorf("error starting EC2 Instance (%s): %w", d.Id(), err)
	}

	if err := WaitForInstanceRunning(conn, d.Id(), InstanceStartTimeout); err != nil {
		return err
	}

	return nil
}

// InstanceStateRefreshFunc returns a resource.StateRefreshFunc that is used to watch
// an EC2 instance.
func InstanceStateRefreshFunc(conn *ec2.EC2, instanceID string, failStates []string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		instance, err := InstanceFindByID(conn, instanceID)
		if err != nil {
			if !tfawserr.ErrMessageContains(err, "InvalidInstanceID.NotFound", "") {
				log.Printf("Error on InstanceStateRefresh: %s", err)
				return nil, "", err
			}
		}

		if instance == nil || instance.State == nil {
			// Sometimes AWS just has consistency issues and doesn't see
			// our instance yet. Return an empty state.
			return nil, "", nil
		}

		state := aws.StringValue(instance.State.Name)

		for _, failState := range failStates {
			if state == failState {
				return instance, state, fmt.Errorf("Failed to reach target state. Reason: %s",
					stringifyStateReason(instance.StateReason))
			}
		}

		return instance, state, nil
	}
}

// MetadataOptionsRefreshFunc returns a resource.StateRefreshFunc that is used to watch
// changes in an EC2 instance's metadata options.
func MetadataOptionsRefreshFunc(conn *ec2.EC2, instanceID string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		instance, err := InstanceFindByID(conn, instanceID)
		if err != nil {
			if !tfawserr.ErrMessageContains(err, "InvalidInstanceID.NotFound", "") {
				log.Printf("Error on InstanceStateRefresh: %s", err)
				return nil, "", err
			}
		}

		if instance == nil || instance.MetadataOptions == nil {
			// Sometimes AWS just has consistency issues and doesn't see
			// our instance yet. Return an empty state.
			return nil, "", nil
		}

		state := aws.StringValue(instance.MetadataOptions.State)

		return instance, state, nil
	}
}

// RootBlockDeviceDeleteOnTerminationRefreshFunc returns a resource.StateRefreshFunc
// that is used to watch changes in an EC2 instance's root block device's delete on termination attribute.
func RootBlockDeviceDeleteOnTerminationRefreshFunc(conn *ec2.EC2, instanceID string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		instance, err := InstanceFindByID(conn, instanceID)
		if err != nil {
			if !tfawserr.ErrMessageContains(err, "InvalidInstanceID.NotFound", "") {
				log.Printf("Error on InstanceStateRefresh: %s", err)
				return nil, "", err
			}
		}

		if instance == nil || len(instance.BlockDeviceMappings) == 0 {
			// Sometimes AWS just has consistency issues and doesn't see
			// our instance yet. Return an empty state.
			return nil, "", nil
		}

		var deleteOnTermination string
		for _, bd := range instance.BlockDeviceMappings {
			if blockDeviceIsRoot(bd, instance) {
				deleteOnTermination = strconv.FormatBool(aws.BoolValue(bd.Ebs.DeleteOnTermination))
				break
			}
		}

		return instance, deleteOnTermination, nil
	}
}

// VolumeStateRefreshFunc returns a resource.StateRefreshFunc that is used to watch
// an EC2 root device volume.
func VolumeStateRefreshFunc(conn *ec2.EC2, volumeID, failState string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		resp, err := conn.DescribeVolumesModifications(&ec2.DescribeVolumesModificationsInput{
			VolumeIds: []*string{aws.String(volumeID)},
		})
		if err != nil {
			if tfawserr.ErrMessageContains(err, "InvalidVolumeID.NotFound", "does not exist") {
				return nil, "", nil
			}
			log.Printf("Error on VolumeStateRefresh: %s", err)
			return nil, "", err
		}
		if resp == nil || len(resp.VolumesModifications) == 0 || resp.VolumesModifications[0] == nil {
			return nil, "", nil
		}

		i := resp.VolumesModifications[0]
		state := aws.StringValue(i.ModificationState)
		if state == failState {
			return i, state, fmt.Errorf("Failed to reach target state. Reason: %s", aws.StringValue(i.StatusMessage))
		}

		return i, state, nil
	}
}

func stringifyStateReason(sr *ec2.StateReason) string {
	if sr.Message != nil {
		return aws.StringValue(sr.Message)
	}
	if sr.Code != nil {
		return aws.StringValue(sr.Code)
	}

	return sr.String()
}

func readBlockDevices(d *schema.ResourceData, instance *ec2.Instance, conn *ec2.EC2) error {
	ibds, err := readBlockDevicesFromInstance(d, instance, conn)
	if err != nil {
		return err
	}

	// Special handling for instances where the only block device is the root device:
	// The call to readBlockDevicesFromInstance above will return the block device
	// in ibds["root"] not ibds["ebs"], thus to set the state correctly,
	// the root block device must be copied over to ibds["ebs"]
	if ibds != nil {
		if _, ok := d.GetOk("ebs_block_device"); ok {
			if v, ok := ibds["ebs"].([]map[string]interface{}); ok && len(v) == 0 {
				if root, ok := ibds["root"].(map[string]interface{}); ok {
					// Make deep copy of data
					m := make(map[string]interface{})

					for k, v := range root {
						m[k] = v
					}

					if snapshotID, ok := ibds["snapshot_id"].(string); ok {
						m["snapshot_id"] = snapshotID
					}

					ibds["ebs"] = []interface{}{m}
				}
			}
		}
	}

	if err := d.Set("ebs_block_device", ibds["ebs"]); err != nil {
		return err
	}

	// This handles the import case which needs to be defaulted to empty
	if _, ok := d.GetOk("root_block_device"); !ok {
		if err := d.Set("root_block_device", []interface{}{}); err != nil {
			return err
		}
	}

	if ibds["root"] != nil {
		roots := []interface{}{ibds["root"]}
		if err := d.Set("root_block_device", roots); err != nil {
			return err
		}
	}

	return nil
}

func associateInstanceProfile(d *schema.ResourceData, conn *ec2.EC2) error {
	input := &ec2.AssociateIamInstanceProfileInput{
		InstanceId: aws.String(d.Id()),
		IamInstanceProfile: &ec2.IamInstanceProfileSpecification{
			Name: aws.String(d.Get("iam_instance_profile").(string)),
		},
	}
	err := resource.Retry(tfiam.PropagationTimeout, func() *resource.RetryError {
		_, err := conn.AssociateIamInstanceProfile(input)
		if err != nil {
			if tfawserr.ErrMessageContains(err, "InvalidParameterValue", "Invalid IAM Instance Profile") {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})
	if tfresource.TimedOut(err) {
		_, err = conn.AssociateIamInstanceProfile(input)
	}
	if err != nil {
		return fmt.Errorf("error associating instance with instance profile: %s", err)
	}
	return nil
}

func disassociateInstanceProfile(associationId *string, conn *ec2.EC2) error {
	_, err := conn.DisassociateIamInstanceProfile(&ec2.DisassociateIamInstanceProfileInput{
		AssociationId: associationId,
	})
	if err != nil {
		return fmt.Errorf("error disassociating instance with instance profile: %w", err)
	}
	return nil
}

func readBlockDevicesFromInstance(d *schema.ResourceData, instance *ec2.Instance, conn *ec2.EC2) (map[string]interface{}, error) {
	blockDevices := make(map[string]interface{})
	blockDevices["ebs"] = make([]map[string]interface{}, 0)
	blockDevices["root"] = nil
	// Ephemeral devices don't show up in BlockDeviceMappings or DescribeVolumes so we can't actually set them

	instanceBlockDevices := make(map[string]*ec2.InstanceBlockDeviceMapping)
	for _, bd := range instance.BlockDeviceMappings {
		if bd.Ebs != nil {
			instanceBlockDevices[aws.StringValue(bd.Ebs.VolumeId)] = bd
		}
	}

	if len(instanceBlockDevices) == 0 {
		return nil, nil
	}

	volIDs := make([]*string, 0, len(instanceBlockDevices))
	for volID := range instanceBlockDevices {
		volIDs = append(volIDs, aws.String(volID))
	}

	// Need to call DescribeVolumes to get volume_size and volume_type for each
	// EBS block device
	volResp, err := conn.DescribeVolumes(&ec2.DescribeVolumesInput{
		VolumeIds: volIDs,
	})
	if err != nil {
		return nil, err
	}

	for _, vol := range volResp.Volumes {
		instanceBd := instanceBlockDevices[aws.StringValue(vol.VolumeId)]
		bd := make(map[string]interface{})

		bd["volume_id"] = aws.StringValue(vol.VolumeId)

		if instanceBd.Ebs != nil && instanceBd.Ebs.DeleteOnTermination != nil {
			bd["delete_on_termination"] = aws.BoolValue(instanceBd.Ebs.DeleteOnTermination)
		}
		if vol.Size != nil {
			bd["volume_size"] = aws.Int64Value(vol.Size)
		}
		if vol.VolumeType != nil {
			bd["volume_type"] = aws.StringValue(vol.VolumeType)
		}
		if vol.Iops != nil {
			bd["iops"] = aws.Int64Value(vol.Iops)
		}
		if vol.Encrypted != nil {
			bd["encrypted"] = aws.BoolValue(vol.Encrypted)
		}
		if vol.KmsKeyId != nil {
			bd["kms_key_id"] = aws.StringValue(vol.KmsKeyId)
		}
		if vol.Throughput != nil {
			bd["throughput"] = aws.Int64Value(vol.Throughput)
		}
		if instanceBd.DeviceName != nil {
			bd["device_name"] = aws.StringValue(instanceBd.DeviceName)
		}
		if v, ok := d.GetOk("volume_tags"); (!ok || v == nil || len(v.(map[string]interface{})) == 0) && vol.Tags != nil {
			bd["tags"] = KeyValueTags(vol.Tags).IgnoreAWS().Map()
		}

		if blockDeviceIsRoot(instanceBd, instance) {
			blockDevices["root"] = bd
		} else {
			if vol.SnapshotId != nil {
				bd["snapshot_id"] = aws.StringValue(vol.SnapshotId)
			}

			blockDevices["ebs"] = append(blockDevices["ebs"].([]map[string]interface{}), bd)
		}
	}
	// If we determine the root device is the only block device mapping
	// in the instance (including ephemerals) after returning from this function,
	// we'll need to set the ebs_block_device as a clone of the root device
	// with the snapshot_id populated; thus, we store the ID for safe-keeping
	if blockDevices["root"] != nil && len(blockDevices["ebs"].([]map[string]interface{})) == 0 {
		blockDevices["snapshot_id"] = volResp.Volumes[0].SnapshotId
	}

	return blockDevices, nil
}

func blockDeviceIsRoot(bd *ec2.InstanceBlockDeviceMapping, instance *ec2.Instance) bool {
	return bd.DeviceName != nil &&
		instance.RootDeviceName != nil &&
		aws.StringValue(bd.DeviceName) == aws.StringValue(instance.RootDeviceName)
}

func fetchLaunchTemplateAmi(specs []interface{}, conn *ec2.EC2) (string, error) {
	if len(specs) < 1 {
		return "", errors.New("Cannot fetch AMI for blank launch template.")
	}

	spec := specs[0].(map[string]interface{})

	idValue, idOk := spec["id"]
	nameValue, nameOk := spec["name"]

	request := &ec2.DescribeLaunchTemplateVersionsInput{}

	if idOk && idValue != "" {
		request.LaunchTemplateId = aws.String(idValue.(string))
	} else if nameOk && nameValue != "" {
		request.LaunchTemplateName = aws.String(nameValue.(string))
	}

	var isLatest bool
	defaultFilter := []*ec2.Filter{
		{
			Name:   aws.String("is-default-version"),
			Values: aws.StringSlice([]string{"true"}),
		},
	}
	if v, ok := spec["version"]; ok && v != "" {
		switch v {
		case "$Default":
			request.Filters = defaultFilter
		case "$Latest":
			isLatest = true
		default:
			request.Versions = []*string{aws.String(v.(string))}
		}
	}

	dltv, err := conn.DescribeLaunchTemplateVersions(request)
	if err != nil {
		return "", err
	}

	var ltData *ec2.ResponseLaunchTemplateData
	if isLatest {
		index := len(dltv.LaunchTemplateVersions) - 1
		ltData = dltv.LaunchTemplateVersions[index].LaunchTemplateData
	} else {
		ltData = dltv.LaunchTemplateVersions[0].LaunchTemplateData
	}

	if ltData.ImageId != nil {
		return *ltData.ImageId, nil
	}

	return "", nil
}

func FetchRootDeviceName(ami string, conn *ec2.EC2) (*string, error) {
	if ami == "" {
		return nil, errors.New("Cannot fetch root device name for blank AMI ID.")
	}

	log.Printf("[DEBUG] Describing AMI %q to get root block device name", ami)
	res, err := conn.DescribeImages(&ec2.DescribeImagesInput{
		ImageIds: []*string{aws.String(ami)},
	})
	if err != nil {
		return nil, err
	}

	// For a bad image, we just return nil so we don't block a refresh
	if len(res.Images) == 0 {
		return nil, nil
	}

	image := res.Images[0]
	rootDeviceName := image.RootDeviceName

	// Instance store backed AMIs do not provide a root device name.
	if aws.StringValue(image.RootDeviceType) == ec2.DeviceTypeInstanceStore {
		return nil, nil
	}

	// Some AMIs have a RootDeviceName like "/dev/sda1" that does not appear as a
	// DeviceName in the BlockDeviceMapping list (which will instead have
	// something like "/dev/sda")
	//
	// While this seems like it breaks an invariant of AMIs, it ends up working
	// on the AWS side, and AMIs like this are common enough that we need to
	// special case it so Terraform does the right thing.
	//
	// Our heuristic is: if the RootDeviceName does not appear in the
	// BlockDeviceMapping, assume that the DeviceName of the first
	// BlockDeviceMapping entry serves as the root device.
	rootDeviceNameInMapping := false
	for _, bdm := range image.BlockDeviceMappings {
		if aws.StringValue(bdm.DeviceName) == aws.StringValue(image.RootDeviceName) {
			rootDeviceNameInMapping = true
		}
	}

	if !rootDeviceNameInMapping && len(image.BlockDeviceMappings) > 0 {
		rootDeviceName = image.BlockDeviceMappings[0].DeviceName
	}

	if rootDeviceName == nil {
		return nil, fmt.Errorf("Error finding Root Device Name for AMI (%s)", ami)
	}

	return rootDeviceName, nil
}

func buildNetworkInterfaceOpts(d *schema.ResourceData, groups []*string, nInterfaces interface{}) []*ec2.InstanceNetworkInterfaceSpecification {
	networkInterfaces := []*ec2.InstanceNetworkInterfaceSpecification{}
	// Get necessary items
	subnet, hasSubnet := d.GetOk("subnet_id")

	if hasSubnet {
		// If we have a non-default VPC / Subnet specified, we can flag
		// AssociatePublicIpAddress to get a Public IP assigned. By default these are not provided.
		// You cannot specify both SubnetId and the NetworkInterface.0.* parameters though, otherwise
		// you get: Network interfaces and an instance-level subnet ID may not be specified on the same request
		// You also need to attach Security Groups to the NetworkInterface instead of the instance,
		// to avoid: Network interfaces and an instance-level security groups may not be specified on
		// the same request
		ni := &ec2.InstanceNetworkInterfaceSpecification{
			DeviceIndex: aws.Int64(0),
			SubnetId:    aws.String(subnet.(string)),
			Groups:      groups,
		}

		if v, ok := d.GetOkExists("associate_public_ip_address"); ok {
			ni.AssociatePublicIpAddress = aws.Bool(v.(bool))
		}

		if v, ok := d.GetOk("private_ip"); ok {
			ni.PrivateIpAddress = aws.String(v.(string))
		}

		if v, ok := d.GetOk("secondary_private_ips"); ok && v.(*schema.Set).Len() > 0 {
			ni.PrivateIpAddresses = expandSecondaryPrivateIPAddresses(v.(*schema.Set).List())
		}

		if v, ok := d.GetOk("ipv6_address_count"); ok {
			ni.Ipv6AddressCount = aws.Int64(int64(v.(int)))
		}

		if v, ok := d.GetOk("ipv6_addresses"); ok {
			ipv6Addresses := make([]*ec2.InstanceIpv6Address, len(v.([]interface{})))
			for i, address := range v.([]interface{}) {
				ipv6Addresses[i] = &ec2.InstanceIpv6Address{
					Ipv6Address: aws.String(address.(string)),
				}
			}

			ni.Ipv6Addresses = ipv6Addresses
		}

		if v := d.Get("vpc_security_group_ids").(*schema.Set); v.Len() > 0 {
			for _, v := range v.List() {
				ni.Groups = append(ni.Groups, aws.String(v.(string)))
			}
		}

		networkInterfaces = append(networkInterfaces, ni)
	} else {
		// If we have manually specified network interfaces, build and attach those here.
		vL := nInterfaces.(*schema.Set).List()
		for _, v := range vL {
			ini := v.(map[string]interface{})
			ni := &ec2.InstanceNetworkInterfaceSpecification{
				DeviceIndex:         aws.Int64(int64(ini["device_index"].(int))),
				NetworkInterfaceId:  aws.String(ini["network_interface_id"].(string)),
				DeleteOnTermination: aws.Bool(ini["delete_on_termination"].(bool)),
			}
			networkInterfaces = append(networkInterfaces, ni)
		}
	}

	return networkInterfaces
}

func readBlockDeviceMappingsFromConfig(d *schema.ResourceData, conn *ec2.EC2) ([]*ec2.BlockDeviceMapping, error) {
	blockDevices := make([]*ec2.BlockDeviceMapping, 0)

	if v, ok := d.GetOk("ebs_block_device"); ok {
		vL := v.(*schema.Set).List()
		for _, v := range vL {
			bd := v.(map[string]interface{})
			ebs := &ec2.EbsBlockDevice{
				DeleteOnTermination: aws.Bool(bd["delete_on_termination"].(bool)),
			}

			if v, ok := bd["snapshot_id"].(string); ok && v != "" {
				ebs.SnapshotId = aws.String(v)
			}

			if v, ok := bd["encrypted"].(bool); ok && v {
				ebs.Encrypted = aws.Bool(v)
			}

			if v, ok := bd["kms_key_id"].(string); ok && v != "" {
				ebs.KmsKeyId = aws.String(v)
			}

			if v, ok := bd["volume_size"].(int); ok && v != 0 {
				ebs.VolumeSize = aws.Int64(int64(v))
			}

			if v, ok := bd["volume_type"].(string); ok && v != "" {
				ebs.VolumeType = aws.String(v)
				if iops, ok := bd["iops"].(int); ok && iops > 0 {
					if ec2.VolumeTypeIo1 == strings.ToLower(v) || ec2.VolumeTypeIo2 == strings.ToLower(v) || ec2.VolumeTypeGp3 == strings.ToLower(v) {
						// Condition: This parameter is required for requests to create io1 or io2
						// volumes and optional for gp3; it is not used in requests to create gp2, st1, sc1, or
						// standard volumes.
						// See: http://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_EbsBlockDevice.html
						ebs.Iops = aws.Int64(int64(iops))
					} else {
						// Enforce IOPs usage with a valid volume type
						// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/12667
						return nil, fmt.Errorf("error creating resource: iops attribute not supported for ebs_block_device with volume_type %s", v)
					}
				}
				if throughput, ok := bd["throughput"].(int); ok && throughput > 0 {
					// `throughput` is only valid for gp3
					if ec2.VolumeTypeGp3 == strings.ToLower(v) {
						ebs.Throughput = aws.Int64(int64(throughput))
					} else {
						return nil, fmt.Errorf("error creating resource: throughput attribute not supported for ebs_block_device with volume_type %s", v)
					}
				}
			}

			blockDevices = append(blockDevices, &ec2.BlockDeviceMapping{
				DeviceName: aws.String(bd["device_name"].(string)),
				Ebs:        ebs,
			})
		}
	}

	if v, ok := d.GetOk("ephemeral_block_device"); ok {
		vL := v.(*schema.Set).List()
		for _, v := range vL {
			bd := v.(map[string]interface{})
			bdm := &ec2.BlockDeviceMapping{
				DeviceName:  aws.String(bd["device_name"].(string)),
				VirtualName: aws.String(bd["virtual_name"].(string)),
			}
			if v, ok := bd["no_device"].(bool); ok && v {
				bdm.NoDevice = aws.String("")
				// When NoDevice is true, just ignore VirtualName since it's not needed
				bdm.VirtualName = nil
			}

			if bdm.NoDevice == nil && aws.StringValue(bdm.VirtualName) == "" {
				return nil, errors.New("virtual_name cannot be empty when no_device is false or undefined.")
			}

			blockDevices = append(blockDevices, bdm)
		}
	}

	if v, ok := d.GetOk("root_block_device"); ok {
		vL := v.([]interface{})
		for _, v := range vL {
			bd := v.(map[string]interface{})
			ebs := &ec2.EbsBlockDevice{
				DeleteOnTermination: aws.Bool(bd["delete_on_termination"].(bool)),
			}

			if v, ok := bd["encrypted"].(bool); ok && v {
				ebs.Encrypted = aws.Bool(v)
			}

			if v, ok := bd["kms_key_id"].(string); ok && v != "" {
				ebs.KmsKeyId = aws.String(bd["kms_key_id"].(string))
			}

			if v, ok := bd["volume_size"].(int); ok && v != 0 {
				ebs.VolumeSize = aws.Int64(int64(v))
			}

			if v, ok := bd["volume_type"].(string); ok && v != "" {
				ebs.VolumeType = aws.String(v)
				if iops, ok := bd["iops"].(int); ok && iops > 0 {
					if ec2.VolumeTypeIo1 == strings.ToLower(v) || ec2.VolumeTypeIo2 == strings.ToLower(v) || ec2.VolumeTypeGp3 == strings.ToLower(v) {
						// Only set the iops attribute if the volume type is io1, io2, or gp3. Setting otherwise
						// can trigger a refresh/plan loop based on the computed value that is given
						// from AWS, and prevent us from specifying 0 as a valid iops.
						//   See https://github.com/hashicorp/terraform/pull/4146
						//   See https://github.com/hashicorp/terraform/issues/7765
						ebs.Iops = aws.Int64(int64(iops))
					} else {
						// Enforce IOPs usage with a valid volume type
						// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/12667
						return nil, fmt.Errorf("error creating resource: iops attribute not supported for root_block_device with volume_type %s", v)
					}
				}
				if throughput, ok := bd["throughput"].(int); ok && throughput > 0 {
					// throughput is only valid for gp3
					if ec2.VolumeTypeGp3 == strings.ToLower(v) {
						ebs.Throughput = aws.Int64(int64(throughput))
					} else {
						// Enforce throughput usage with a valid volume type
						return nil, fmt.Errorf("error creating resource: throughput attribute not supported for root_block_device with volume_type %s", v)
					}
				}
			}

			var ami string
			if v, ok := d.GetOk("launch_template"); ok {
				var err error
				ami, err = fetchLaunchTemplateAmi(v.([]interface{}), conn)
				if err != nil {
					return nil, err
				}
			}

			// AMI id from attributes overrides ami from launch template
			if v, ok := d.GetOk("ami"); ok {
				ami = v.(string)
			}

			if ami == "" {
				return nil, errors.New("`ami` must be set or provided via launch template")
			}

			if dn, err := FetchRootDeviceName(ami, conn); err == nil {
				if dn == nil {
					return nil, fmt.Errorf(
						"Expected 1 AMI for ID: %s, got none",
						ami)
				}

				blockDevices = append(blockDevices, &ec2.BlockDeviceMapping{
					DeviceName: dn,
					Ebs:        ebs,
				})
			} else {
				return nil, err
			}
		}
	}

	return blockDevices, nil
}

func readVolumeTags(conn *ec2.EC2, instanceId string) ([]*ec2.Tag, error) {
	volumeIds, err := getInstanceVolumeIDs(conn, instanceId)
	if err != nil {
		return nil, err
	}

	resp, err := conn.DescribeTags(&ec2.DescribeTagsInput{
		Filters: ec2AttributeFiltersFromMultimap(map[string][]string{
			"resource-id": volumeIds,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting tags for volumes (%s): %s", volumeIds, err)
	}

	return ec2TagsFromTagDescriptions(resp.Tags), nil
}

// Determine whether we're referring to security groups with
// IDs or names. We use a heuristic to figure this out. By default,
// we use IDs if we're in a VPC, and names otherwise (EC2-Classic).
// However, the default VPC accepts either, so store them both here and let the
// config determine which one to use in Plan and Apply.
func readSecurityGroups(d *schema.ResourceData, instance *ec2.Instance, conn *ec2.EC2) error {
	// An instance with a subnet is in a VPC; an instance without a subnet is in EC2-Classic.
	hasSubnet := aws.StringValue(instance.SubnetId) != ""
	useID, useName := hasSubnet, !hasSubnet

	// If the instance is in a VPC, find out if that VPC is Default to determine
	// whether to store names.
	if vpcID := aws.StringValue(instance.VpcId); vpcID != "" {
		vpc, err := FindVPCByID(conn, vpcID)

		if err != nil {
			log.Printf("[WARN] error reading EC2 Instance (%s) VPC (%s): %s", d.Id(), vpcID, err)
		} else {
			useName = aws.BoolValue(vpc.IsDefault)
		}
	}

	// Build up the security groups
	if useID {
		sgs := make([]string, 0, len(instance.SecurityGroups))
		for _, sg := range instance.SecurityGroups {
			sgs = append(sgs, aws.StringValue(sg.GroupId))
		}
		log.Printf("[DEBUG] Setting Security Group IDs: %#v", sgs)
		if err := d.Set("vpc_security_group_ids", sgs); err != nil {
			return err
		}
	} else {
		if err := d.Set("vpc_security_group_ids", []string{}); err != nil {
			return err
		}
	}
	if useName {
		sgs := make([]string, 0, len(instance.SecurityGroups))
		for _, sg := range instance.SecurityGroups {
			sgs = append(sgs, aws.StringValue(sg.GroupName))
		}
		log.Printf("[DEBUG] Setting Security Group Names: %#v", sgs)
		if err := d.Set("security_groups", sgs); err != nil {
			return err
		}
	} else {
		if err := d.Set("security_groups", []string{}); err != nil {
			return err
		}
	}
	return nil
}

func readInstanceShutdownBehavior(d *schema.ResourceData, conn *ec2.EC2) error {
	output, err := conn.DescribeInstanceAttribute(&ec2.DescribeInstanceAttributeInput{
		InstanceId: aws.String(d.Id()),
		Attribute:  aws.String(ec2.InstanceAttributeNameInstanceInitiatedShutdownBehavior),
	})

	if err != nil {
		return fmt.Errorf("error while describing instance (%s) attribute (%s): %w", d.Id(), ec2.InstanceAttributeNameInstanceInitiatedShutdownBehavior, err)
	}

	if output != nil && output.InstanceInitiatedShutdownBehavior != nil {
		d.Set("instance_initiated_shutdown_behavior", output.InstanceInitiatedShutdownBehavior.Value)
	}

	return nil
}

func getInstancePasswordData(instanceID string, conn *ec2.EC2) (string, error) {
	log.Printf("[INFO] Reading password data for instance %s", instanceID)

	var passwordData string
	var resp *ec2.GetPasswordDataOutput
	input := &ec2.GetPasswordDataInput{
		InstanceId: aws.String(instanceID),
	}
	err := resource.Retry(15*time.Minute, func() *resource.RetryError {
		var err error
		resp, err = conn.GetPasswordData(input)

		if err != nil {
			return resource.NonRetryableError(err)
		}

		if resp.PasswordData == nil || aws.StringValue(resp.PasswordData) == "" {
			return resource.RetryableError(fmt.Errorf("Password data is blank for instance ID: %s", instanceID))
		}

		passwordData = strings.TrimSpace(aws.StringValue(resp.PasswordData))

		log.Printf("[INFO] Password data read for instance %s", instanceID)
		return nil
	})
	if tfresource.TimedOut(err) {
		resp, err = conn.GetPasswordData(input)
		if err != nil {
			return "", fmt.Errorf("Error getting password data: %s", err)
		}
		if resp.PasswordData == nil || aws.StringValue(resp.PasswordData) == "" {
			return "", fmt.Errorf("Password data is blank for instance ID: %s", instanceID)
		}
		passwordData = strings.TrimSpace(aws.StringValue(resp.PasswordData))
	}
	if err != nil {
		return "", err
	}

	return passwordData, nil
}

type awsInstanceOpts struct {
	BlockDeviceMappings               []*ec2.BlockDeviceMapping
	CapacityReservationSpecification  *ec2.CapacityReservationSpecification
	DisableAPITermination             *bool
	EBSOptimized                      *bool
	Monitoring                        *ec2.RunInstancesMonitoringEnabled
	IAMInstanceProfile                *ec2.IamInstanceProfileSpecification
	ImageID                           *string
	InstanceInitiatedShutdownBehavior *string
	InstanceType                      *string
	Ipv6AddressCount                  *int64
	Ipv6Addresses                     []*ec2.InstanceIpv6Address
	KeyName                           *string
	LaunchTemplate                    *ec2.LaunchTemplateSpecification
	NetworkInterfaces                 []*ec2.InstanceNetworkInterfaceSpecification
	Placement                         *ec2.Placement
	PrivateIPAddress                  *string
	SecurityGroupIDs                  []*string
	SecurityGroups                    []*string
	SpotPlacement                     *ec2.SpotPlacement
	SubnetID                          *string
	UserData64                        *string
	CreditSpecification               *ec2.CreditSpecificationRequest
	CpuOptions                        *ec2.CpuOptionsRequest
	HibernationOptions                *ec2.HibernationOptionsRequest
	MetadataOptions                   *ec2.InstanceMetadataOptionsRequest
	EnclaveOptions                    *ec2.EnclaveOptionsRequest
}

func buildInstanceOpts(d *schema.ResourceData, meta interface{}) (*awsInstanceOpts, error) {
	conn := meta.(*conns.AWSClient).EC2Conn

	opts := &awsInstanceOpts{
		DisableAPITermination: aws.Bool(d.Get("disable_api_termination").(bool)),
		EBSOptimized:          aws.Bool(d.Get("ebs_optimized").(bool)),
		MetadataOptions:       expandEc2InstanceMetadataOptions(d.Get("metadata_options").([]interface{})),
		EnclaveOptions:        expandEc2EnclaveOptions(d.Get("enclave_options").([]interface{})),
	}

	if v, ok := d.GetOk("ami"); ok {
		opts.ImageID = aws.String(v.(string))
	}

	if v, ok := d.GetOk("instance_type"); ok {
		opts.InstanceType = aws.String(v.(string))
	}

	if v, ok := d.GetOk("launch_template"); ok {
		opts.LaunchTemplate = expandEc2LaunchTemplateSpecification(v.([]interface{}))
	}

	instanceType := d.Get("instance_type").(string)

	// Set default cpu_credits as Unlimited for T3 instance type
	if strings.HasPrefix(instanceType, "t3") {
		opts.CreditSpecification = &ec2.CreditSpecificationRequest{
			CpuCredits: aws.String("unlimited"),
		}
	}

	if v, ok := d.GetOk("credit_specification"); ok {
		// Only T2 and T3 are burstable performance instance types and supports Unlimited
		if strings.HasPrefix(instanceType, "t2") || strings.HasPrefix(instanceType, "t3") {
			if cs, ok := v.([]interface{})[0].(map[string]interface{}); ok {
				opts.CreditSpecification = &ec2.CreditSpecificationRequest{
					CpuCredits: aws.String(cs["cpu_credits"].(string)),
				}
			} else {
				log.Print("[WARN] credit_specification is defined but the value of cpu_credits is missing, default value will be used.")
			}
		} else {
			log.Print("[WARN] credit_specification is defined but instance type is not T2/T3. Ignoring...")
		}
	}

	if v := d.Get("instance_initiated_shutdown_behavior").(string); v != "" {
		opts.InstanceInitiatedShutdownBehavior = aws.String(v)
	}

	opts.Monitoring = &ec2.RunInstancesMonitoringEnabled{
		Enabled: aws.Bool(d.Get("monitoring").(bool)),
	}

	opts.IAMInstanceProfile = &ec2.IamInstanceProfileSpecification{
		Name: aws.String(d.Get("iam_instance_profile").(string)),
	}

	userData := d.Get("user_data").(string)
	userDataBase64 := d.Get("user_data_base64").(string)

	if userData != "" {
		opts.UserData64 = aws.String(verify.Base64Encode([]byte(userData)))
	} else if userDataBase64 != "" {
		opts.UserData64 = aws.String(userDataBase64)
	}

	// check for non-default Subnet, and cast it to a String
	subnet, hasSubnet := d.GetOk("subnet_id")
	subnetID := subnet.(string)

	// Placement is used for aws_instance; SpotPlacement is used for
	// aws_spot_instance_request. They represent the same data. :-|
	opts.Placement = &ec2.Placement{
		AvailabilityZone: aws.String(d.Get("availability_zone").(string)),
		GroupName:        aws.String(d.Get("placement_group").(string)),
	}

	if v, ok := d.GetOk("placement_partition_number"); ok {
		opts.Placement.PartitionNumber = aws.Int64(int64(v.(int)))
	}

	opts.SpotPlacement = &ec2.SpotPlacement{
		AvailabilityZone: aws.String(d.Get("availability_zone").(string)),
		GroupName:        aws.String(d.Get("placement_group").(string)),
	}

	if v := d.Get("tenancy").(string); v != "" {
		opts.Placement.Tenancy = aws.String(v)
	}
	if v := d.Get("host_id").(string); v != "" {
		opts.Placement.HostId = aws.String(v)
	}

	if v := d.Get("cpu_core_count").(int); v > 0 {
		tc := d.Get("cpu_threads_per_core").(int)
		if tc < 0 {
			tc = 2
		}
		opts.CpuOptions = &ec2.CpuOptionsRequest{
			CoreCount:      aws.Int64(int64(v)),
			ThreadsPerCore: aws.Int64(int64(tc)),
		}
	}

	if v := d.Get("hibernation"); v != "" {
		opts.HibernationOptions = &ec2.HibernationOptionsRequest{
			Configured: aws.Bool(v.(bool)),
		}
	}

	var groups []*string
	if v := d.Get("security_groups"); v != nil {
		// Security group names.
		// For a nondefault VPC, you must use security group IDs instead.
		// See http://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_RunInstances.html
		sgs := v.(*schema.Set).List()
		if len(sgs) > 0 && hasSubnet {
			log.Print("[WARN] Deprecated. Attempting to use 'security_groups' within a VPC instance. Use 'vpc_security_group_ids' instead.")
		}
		for _, v := range sgs {
			str := v.(string)
			groups = append(groups, aws.String(str))
		}
	}

	networkInterfaces, interfacesOk := d.GetOk("network_interface")

	// If setting subnet and public address, OR manual network interfaces, populate those now.
	if hasSubnet || interfacesOk {
		// Otherwise we're attaching (a) network interface(s)
		opts.NetworkInterfaces = buildNetworkInterfaceOpts(d, groups, networkInterfaces)
	} else {
		// If simply specifying a subnetID, privateIP, Security Groups, or VPC Security Groups, build these now
		if subnetID != "" {
			opts.SubnetID = aws.String(subnetID)
		}

		if v, ok := d.GetOk("private_ip"); ok {
			opts.PrivateIPAddress = aws.String(v.(string))
		}
		if opts.SubnetID != nil &&
			aws.StringValue(opts.SubnetID) != "" {
			opts.SecurityGroupIDs = groups
		} else {
			opts.SecurityGroups = groups
		}

		if v, ok := d.GetOk("ipv6_address_count"); ok {
			opts.Ipv6AddressCount = aws.Int64(int64(v.(int)))
		}

		if v, ok := d.GetOk("ipv6_addresses"); ok {
			ipv6Addresses := make([]*ec2.InstanceIpv6Address, len(v.([]interface{})))
			for i, address := range v.([]interface{}) {
				ipv6Addresses[i] = &ec2.InstanceIpv6Address{
					Ipv6Address: aws.String(address.(string)),
				}
			}

			opts.Ipv6Addresses = ipv6Addresses
		}

		if v := d.Get("vpc_security_group_ids").(*schema.Set); v.Len() > 0 {
			for _, v := range v.List() {
				opts.SecurityGroupIDs = append(opts.SecurityGroupIDs, aws.String(v.(string)))
			}
		}
	}

	if v, ok := d.GetOk("key_name"); ok {
		opts.KeyName = aws.String(v.(string))
	}

	blockDevices, err := readBlockDeviceMappingsFromConfig(d, conn)
	if err != nil {
		return nil, err
	}
	if len(blockDevices) > 0 {
		opts.BlockDeviceMappings = blockDevices
	}

	if v, ok := d.GetOk("capacity_reservation_specification"); ok {
		opts.CapacityReservationSpecification = expandCapacityReservationSpecification(v.([]interface{}))
	}

	return opts, nil
}

func terminateInstance(conn *ec2.EC2, id string, timeout time.Duration) error {
	log.Printf("[INFO] Terminating instance: %s", id)
	req := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{aws.String(id)},
	}
	if _, err := conn.TerminateInstances(req); err != nil {
		if tfawserr.ErrMessageContains(err, "InvalidInstanceID.NotFound", "") {
			return nil
		}
		return err
	}

	return waitForInstanceDeletion(conn, id, timeout)
}

func WaitForInstanceRunning(conn *ec2.EC2, id string, timeout time.Duration) error {
	log.Printf("[DEBUG] Waiting for instance (%s) to be running", id)

	stateConf := &resource.StateChangeConf{
		Pending:    []string{ec2.InstanceStateNamePending, ec2.InstanceStateNameStopped},
		Target:     []string{ec2.InstanceStateNameRunning},
		Refresh:    InstanceStateRefreshFunc(conn, id, []string{ec2.InstanceStateNameTerminated}),
		Timeout:    timeout,
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, err := stateConf.WaitForState()
	if err != nil {
		return fmt.Errorf(
			"error waiting for instance (%s) to be running: %s", id, err)
	}

	return nil
}

func WaitForInstanceStopping(conn *ec2.EC2, id string, timeout time.Duration) error {
	log.Printf("[DEBUG] Waiting for instance (%s) to become stopped", id)

	stateConf := &resource.StateChangeConf{
		Pending: []string{ec2.InstanceStateNamePending, ec2.InstanceStateNameRunning,
			ec2.InstanceStateNameShuttingDown, ec2.InstanceStateNameStopped, ec2.InstanceStateNameStopping},
		Target:     []string{ec2.InstanceStateNameStopped},
		Refresh:    InstanceStateRefreshFunc(conn, id, []string{}),
		Timeout:    timeout,
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, err := stateConf.WaitForState()
	if err != nil {
		return fmt.Errorf(
			"error waiting for instance (%s) to stop: %s", id, err)
	}

	return nil
}

func waitForInstanceDeletion(conn *ec2.EC2, id string, timeout time.Duration) error {
	log.Printf("[DEBUG] Waiting for instance (%s) to become terminated", id)

	stateConf := &resource.StateChangeConf{
		Pending: []string{ec2.InstanceStateNamePending, ec2.InstanceStateNameRunning,
			ec2.InstanceStateNameShuttingDown, ec2.InstanceStateNameStopped, ec2.InstanceStateNameStopping},
		Target:     []string{ec2.InstanceStateNameTerminated},
		Refresh:    InstanceStateRefreshFunc(conn, id, []string{}),
		Timeout:    timeout,
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, err := stateConf.WaitForState()
	if err != nil {
		return fmt.Errorf(
			"Error waiting for instance (%s) to terminate: %s", id, err)
	}

	return nil
}

func userDataHashSum(user_data string) string {
	// Check whether the user_data is not Base64 encoded.
	// Always calculate hash of base64 decoded value since we
	// check against double-encoding when setting it
	v, base64DecodeError := base64.StdEncoding.DecodeString(user_data)
	if base64DecodeError != nil {
		v = []byte(user_data)
	}

	hash := sha1.Sum(v)
	return hex.EncodeToString(hash[:])
}

func getInstanceVolumeIDs(conn *ec2.EC2, instanceId string) ([]string, error) {
	volumeIds := []string{}

	resp, err := conn.DescribeVolumes(&ec2.DescribeVolumesInput{
		Filters: BuildAttributeFilterList(map[string]string{
			"attachment.instance-id": instanceId,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting volumes for instance (%s): %s", instanceId, err)
	}

	for _, v := range resp.Volumes {
		volumeIds = append(volumeIds, aws.StringValue(v.VolumeId))
	}

	return volumeIds, nil
}

func getRootVolumeId(instance *ec2.Instance) string {
	rootVolumeId := ""
	for _, bd := range instance.BlockDeviceMappings {
		if bd.Ebs != nil && blockDeviceIsRoot(bd, instance) {
			if bd.Ebs.VolumeId != nil {
				rootVolumeId = aws.StringValue(bd.Ebs.VolumeId)
			}
			break
		}
	}

	return rootVolumeId
}

func getVolumeIdByDeviceName(instance *ec2.Instance, deviceName string) string {
	volumeId := ""
	for _, bd := range instance.BlockDeviceMappings {
		if aws.StringValue(bd.DeviceName) == deviceName {
			if bd.Ebs != nil {
				volumeId = aws.StringValue(bd.Ebs.VolumeId)
				break
			}
		}
	}

	return volumeId
}

func blockDeviceTagsDefined(d *schema.ResourceData) bool {
	if v, ok := d.GetOk("root_block_device"); ok {
		vL := v.([]interface{})
		for _, v := range vL {
			bd := v.(map[string]interface{})
			if blockDeviceTags, ok := bd["tags"].(map[string]interface{}); ok && len(blockDeviceTags) > 0 {
				return true
			}
		}
	}

	if v, ok := d.GetOk("ebs_block_device"); ok {
		vL := v.(*schema.Set).List()
		for _, v := range vL {
			bd := v.(map[string]interface{})
			if blockDeviceTags, ok := bd["tags"].(map[string]interface{}); ok && len(blockDeviceTags) > 0 {
				return true
			}
		}
	}

	return false
}

func getCreditSpecifications(conn *ec2.EC2, instanceId string) ([]map[string]interface{}, error) {
	var creditSpecifications []map[string]interface{}
	creditSpecification := make(map[string]interface{})

	attr, err := conn.DescribeInstanceCreditSpecifications(&ec2.DescribeInstanceCreditSpecificationsInput{
		InstanceIds: []*string{aws.String(instanceId)},
	})
	if err != nil {
		return creditSpecifications, err
	}
	if len(attr.InstanceCreditSpecifications) > 0 {
		creditSpecification["cpu_credits"] = aws.StringValue(attr.InstanceCreditSpecifications[0].CpuCredits)
		creditSpecifications = append(creditSpecifications, creditSpecification)
	}

	return creditSpecifications, nil
}

func expandEc2InstanceMetadataOptions(l []interface{}) *ec2.InstanceMetadataOptionsRequest {
	if len(l) == 0 || l[0] == nil {
		return nil
	}

	m := l[0].(map[string]interface{})

	opts := &ec2.InstanceMetadataOptionsRequest{
		HttpEndpoint: aws.String(m["http_endpoint"].(string)),
	}

	if m["http_endpoint"].(string) == ec2.InstanceMetadataEndpointStateEnabled {
		// These parameters are not allowed unless HttpEndpoint is enabled

		if v, ok := m["http_tokens"].(string); ok && v != "" {
			opts.HttpTokens = aws.String(v)
		}

		if v, ok := m["http_put_response_hop_limit"].(int); ok && v != 0 {
			opts.HttpPutResponseHopLimit = aws.Int64(int64(v))
		}

		if v, ok := m["instance_metadata_tags"].(string); ok && v != "" {
			opts.InstanceMetadataTags = aws.String(v)
		}
	}

	return opts
}

func expandEc2EnclaveOptions(l []interface{}) *ec2.EnclaveOptionsRequest {
	if len(l) == 0 || l[0] == nil {
		return nil
	}

	m := l[0].(map[string]interface{})

	opts := &ec2.EnclaveOptionsRequest{
		Enabled: aws.Bool(m["enabled"].(bool)),
	}

	return opts
}

//Expands an array of secondary Private IPs into a ec2 Private IP Address Spec
func expandSecondaryPrivateIPAddresses(ips []interface{}) []*ec2.PrivateIpAddressSpecification {
	specs := make([]*ec2.PrivateIpAddressSpecification, 0, len(ips))
	for _, v := range ips {
		spec := &ec2.PrivateIpAddressSpecification{
			PrivateIpAddress: aws.String(v.(string)),
		}

		specs = append(specs, spec)
	}
	return specs
}

func expandCapacityReservationSpecification(crs []interface{}) *ec2.CapacityReservationSpecification {
	if len(crs) < 1 || crs[0] == nil {
		return nil
	}

	m := crs[0].(map[string]interface{})

	capacityReservationSpecification := &ec2.CapacityReservationSpecification{}

	if v, ok := m["capacity_reservation_preference"]; ok && v != "" && v != nil {
		capacityReservationSpecification.CapacityReservationPreference = aws.String(v.(string))
	}

	if v, ok := m["capacity_reservation_target"]; ok && v != "" && (len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil) {
		capacityReservationSpecification.CapacityReservationTarget = expandCapacityReservationTarget(v.([]interface{}))
	}

	return capacityReservationSpecification
}

func expandCapacityReservationTarget(crt []interface{}) *ec2.CapacityReservationTarget {
	if len(crt) < 1 || crt[0] == nil {
		return nil
	}

	m := crt[0].(map[string]interface{})

	capacityReservationTarget := &ec2.CapacityReservationTarget{}

	if v, ok := m["capacity_reservation_id"]; ok && v != "" {
		capacityReservationTarget.CapacityReservationId = aws.String(v.(string))
	}

	return capacityReservationTarget
}

func flattenEc2InstanceMetadataOptions(opts *ec2.InstanceMetadataOptionsResponse) []interface{} {
	if opts == nil {
		return nil
	}

	m := map[string]interface{}{
		"http_endpoint":               aws.StringValue(opts.HttpEndpoint),
		"http_put_response_hop_limit": aws.Int64Value(opts.HttpPutResponseHopLimit),
		"http_tokens":                 aws.StringValue(opts.HttpTokens),
		"instance_metadata_tags":      aws.StringValue(opts.InstanceMetadataTags),
	}

	return []interface{}{m}
}

func flattenEc2EnclaveOptions(opts *ec2.EnclaveOptions) []interface{} {
	if opts == nil {
		return nil
	}

	m := map[string]interface{}{
		"enabled": aws.BoolValue(opts.Enabled),
	}

	return []interface{}{m}
}

func flattenCapacityReservationSpecification(crs *ec2.CapacityReservationSpecificationResponse) []interface{} {
	if crs == nil {
		return []interface{}{}
	}

	m := map[string]interface{}{
		"capacity_reservation_preference": aws.StringValue(crs.CapacityReservationPreference),
		"capacity_reservation_target":     flattenCapacityReservationTarget(crs.CapacityReservationTarget),
	}

	return []interface{}{m}
}

func flattenCapacityReservationTarget(crt *ec2.CapacityReservationTargetResponse) []interface{} {
	if crt == nil {
		return []interface{}{}
	}

	m := map[string]interface{}{
		"capacity_reservation_id": aws.StringValue(crt.CapacityReservationId),
	}

	return []interface{}{m}
}

// InstanceFindByID returns the EC2 instance by ID
// * If the instance is found, returns the instance and nil
// * If no instance is found, returns nil and nil
// * If an error occurs, returns nil and the error
func InstanceFindByID(conn *ec2.EC2, id string) (*ec2.Instance, error) {
	instances, err := resourceInstanceFind(conn, &ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{id}),
	})
	if err != nil {
		return nil, err
	}

	if len(instances) == 0 {
		return nil, nil
	}

	return instances[0], nil
}

// resourceInstanceFind returns EC2 instances matching the input parameters
// * If instances are found, returns a slice of instances and nil
// * If no instances are found, returns an empty slice and nil
// * If an error occurs, returns nil and the error
func resourceInstanceFind(conn *ec2.EC2, params *ec2.DescribeInstancesInput) ([]*ec2.Instance, error) {
	resp, err := conn.DescribeInstances(params)
	if err != nil {
		return nil, err
	}

	if len(resp.Reservations) == 0 {
		return []*ec2.Instance{}, nil
	}

	return resp.Reservations[0].Instances, nil
}

func getInstanceLaunchTemplate(conn *ec2.EC2, d *schema.ResourceData) ([]map[string]interface{}, error) {
	attrs := map[string]interface{}{}
	result := make([]map[string]interface{}, 0)

	id, err := getInstanceLaunchTemplateID(conn, d.Id())
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, nil
	}

	name, defaultVersion, latestVersion, err := getLaunchTemplateSpecification(conn, id)

	if err != nil {
		if tfawserr.ErrMessageContains(err, "InvalidLaunchTemplateId.Malformed", "") || tfawserr.ErrMessageContains(err, "InvalidLaunchTemplateId.NotFound", "") {
			// Instance is tagged with non existent template just set it to nil
			log.Printf("[WARN] Launch template %s not found, removing from state", id)
			return nil, nil
		}
		return nil, fmt.Errorf("error reading Launch Template: %s", err)
	}

	attrs["id"] = id
	attrs["name"] = name

	liveVersion, err := getInstanceLaunchTemplateVersion(conn, d.Id())
	if err != nil {
		return nil, err
	}

	dltvi := &ec2.DescribeLaunchTemplateVersionsInput{
		LaunchTemplateId: aws.String(id),
		Versions:         []*string{aws.String(liveVersion)},
	}

	if _, err := conn.DescribeLaunchTemplateVersions(dltvi); err != nil {
		if tfawserr.ErrMessageContains(err, "InvalidLaunchTemplateId.VersionNotFound", "") {
			// Instance is tagged with non existent template version, just don't set it
			log.Printf("[WARN] Launch template %s version %s not found, removing from state", id, liveVersion)
			result = append(result, attrs)
			return result, nil
		}
		return nil, fmt.Errorf("error reading Launch Template Version: %s", err)
	}

	if stateVersion, ok := d.GetOk("launch_template.0.version"); ok {
		switch stateVersion {
		case "$Default":
			if liveVersion == defaultVersion {
				attrs["version"] = "$Default"
			} else {
				attrs["version"] = liveVersion
			}
		case "$Latest":
			if liveVersion == latestVersion {
				attrs["version"] = "$Latest"
			} else {
				attrs["version"] = liveVersion
			}
		default:
			attrs["version"] = liveVersion
		}
	}

	result = append(result, attrs)

	return result, nil
}

func getInstanceLaunchTemplateID(conn *ec2.EC2, instanceId string) (string, error) {
	idTag := "aws:ec2launchtemplate:id"

	launchTemplateId, err := getInstanceTagValue(conn, instanceId, idTag)
	if err != nil {
		return "", fmt.Errorf("error reading Instance Launch Template Id Tag: %s", err)
	}
	if launchTemplateId == nil {
		return "", nil
	}

	return *launchTemplateId, nil
}

func getInstanceLaunchTemplateVersion(conn *ec2.EC2, instanceId string) (string, error) {
	versionTag := "aws:ec2launchtemplate:version"

	launchTemplateVersion, err := getInstanceTagValue(conn, instanceId, versionTag)
	if err != nil {
		return "", fmt.Errorf("error reading Instance Launch Template Version Tag: %s", err)
	}
	if launchTemplateVersion == nil {
		return "", nil
	}

	return *launchTemplateVersion, nil
}

// getLaunchTemplateSpecification takes conn and template id
// returns name, default version, latest version
func getLaunchTemplateSpecification(conn *ec2.EC2, id string) (string, string, string, error) {
	dlt, err := conn.DescribeLaunchTemplates(&ec2.DescribeLaunchTemplatesInput{
		LaunchTemplateIds: []*string{aws.String(id)},
	})
	if err != nil {
		return "", "", "", err
	}

	name := *dlt.LaunchTemplates[0].LaunchTemplateName
	defaultVersion := strconv.FormatInt(*dlt.LaunchTemplates[0].DefaultVersionNumber, 10)
	latestVersion := strconv.FormatInt(*dlt.LaunchTemplates[0].LatestVersionNumber, 10)

	return name, defaultVersion, latestVersion, nil
}

func expandEc2LaunchTemplateSpecification(specs []interface{}) *ec2.LaunchTemplateSpecification {
	if len(specs) < 1 {
		return nil
	}

	spec := specs[0].(map[string]interface{})

	idValue, idOk := spec["id"]
	nameValue, nameOk := spec["name"]

	result := &ec2.LaunchTemplateSpecification{}

	if idOk && idValue != "" {
		result.LaunchTemplateId = aws.String(idValue.(string))
	} else if nameOk && nameValue != "" {
		result.LaunchTemplateName = aws.String(nameValue.(string))
	}

	if v, ok := spec["version"]; ok && v != "" {
		result.Version = aws.String(v.(string))
	}

	return result
}
