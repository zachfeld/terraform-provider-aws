package ec2_test

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/hashicorp/aws-sdk-go-base/v2/awsv1shim/v2/tfawserr"
	sdkacctest "github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-provider-aws/internal/acctest"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	tfec2 "github.com/hashicorp/terraform-provider-aws/internal/service/ec2"
)

func init() {
	acctest.RegisterServiceErrorCheckFunc(ec2.EndpointsID, testAccErrorCheckSkipEC2)

}

func testAccErrorCheckSkipEC2(t *testing.T) resource.ErrorCheckFunc {
	return acctest.ErrorCheckSkipMessagesContaining(t,
		"VolumeTypeNotAvailableInRegion",
		"Invalid value specified for Phase",
	)
}

func TestFetchRootDevice(t *testing.T) {
	cases := []struct {
		label  string
		images []*ec2.Image
		name   string
	}{
		{
			"device name in mappings",
			[]*ec2.Image{{
				RootDeviceType: aws.String("ebs"),
				RootDeviceName: aws.String("/dev/xvda"),
				BlockDeviceMappings: []*ec2.BlockDeviceMapping{
					{DeviceName: aws.String("/dev/xvdb")},
					{DeviceName: aws.String("/dev/xvda")},
				},
			}},
			"/dev/xvda",
		},
		{
			"device name not in mappings",
			[]*ec2.Image{{
				RootDeviceType: aws.String("ebs"),
				RootDeviceName: aws.String("/dev/xvda"),
				BlockDeviceMappings: []*ec2.BlockDeviceMapping{
					{DeviceName: aws.String("/dev/xvdb")},
					{DeviceName: aws.String("/dev/xvdc")},
				},
			}},
			"/dev/xvdb",
		},
		{
			"no images",
			[]*ec2.Image{},
			"",
		},
	}

	sess, err := session.NewSession(nil)
	if err != nil {
		t.Errorf("Error new session: %s", err)
	}

	conn := ec2.New(sess)

	for _, tc := range cases {
		t.Run(fmt.Sprintf(tc.label), func(t *testing.T) {
			conn.Handlers.Clear()
			conn.Handlers.Send.PushBack(func(r *request.Request) {
				data := r.Data.(*ec2.DescribeImagesOutput)
				data.Images = tc.images
			})
			name, _ := tfec2.FetchRootDeviceName("ami-123", conn)
			if tc.name != aws.StringValue(name) {
				t.Errorf("Expected name %s, got %s", tc.name, aws.StringValue(name))
			}
		})
	}
}

func TestAccEC2Instance_inDefaultVPCBySgName(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigInDefaultVpcBySgName(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_inDefaultVPCBySgID(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigInDefaultVpcBySgId(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_inEC2Classic(t *testing.T) {
	resourceName := "aws_instance.test"
	var v ec2.Instance

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:          func() { acctest.PreCheck(t); acctest.PreCheckEC2Classic(t) },
		ErrorCheck:        acctest.ErrorCheck(t, ec2.EndpointsID),
		ProviderFactories: acctest.ProviderFactories,
		CheckDestroy:      testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigInEc2Classic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceEc2ClassicExists(resourceName, &v),
				),
			},
			{
				Config:                  testAccInstanceConfigInEc2Classic(),
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"network_interface", "source_dest_check"},
			},
		},
	})
}

func TestAccEC2Instance_basic(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		// No subnet_id specified requires default VPC with default subnets or EC2-Classic.
		PreCheck: func() {
			acctest.PreCheck(t)
			testAccPreCheckEC2ClassicOrHasDefaultVPCWithDefaultSubnets(t)
		},
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					acctest.MatchResourceAttrRegionalARN(resourceName, "arn", "ec2", regexp.MustCompile(`instance/i-[a-z0-9]+`)),
					resource.TestCheckResourceAttr(resourceName, "instance_initiated_shutdown_behavior", "stop"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
				// Required for EC2-Classic.
				ImportStateVerifyIgnore: []string{"source_dest_check"},
			},
		},
	})
}

func TestAccEC2Instance_atLeastOneOtherEBSVolume(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigAtLeastOneOtherEbsVolume(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "user_data", "3dc39dda39be1205215e776bad998da361a5955d"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.#", "0"), // This is an instance store AMI
					resource.TestCheckResourceAttr(resourceName, "ebs_block_device.#", "0"),
					resource.TestCheckResourceAttr(resourceName, "outpost_arn", ""),
					acctest.MatchResourceAttrRegionalARN(resourceName, "arn", "ec2", regexp.MustCompile(`instance/i-[a-z0-9]+`)),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// We repeat the exact same test so that we can be sure
			// that the user data hash stuff is working without generating
			// an incorrect diff.
			{
				Config: testAccInstanceConfigAtLeastOneOtherEbsVolume(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "user_data", "3dc39dda39be1205215e776bad998da361a5955d"),
					resource.TestCheckResourceAttr(resourceName, "ebs_block_device.#", "0"),
				),
			},
		},
	})
}

func TestAccEC2Instance_EBSBlockDevice_kmsKeyARN(t *testing.T) {
	var instance ec2.Instance
	kmsKeyResourceName := "aws_kms_key.test"
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigEbsBlockDeviceKmsKeyArn(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance),
					resource.TestCheckResourceAttr(resourceName, "ebs_block_device.#", "1"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"encrypted": "true",
					}),
					resource.TestCheckTypeSetElemAttrPair(resourceName, "ebs_block_device.*.kms_key_id", kmsKeyResourceName, "arn"),
				),
			},
		},
	})
}

// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/12667
func TestAccEC2Instance_EBSBlockDevice_invalidIopsForVolumeType(t *testing.T) {
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccInstanceConfigEBSBlockDeviceInvalidIops,
				ExpectError: regexp.MustCompile(`error creating resource: iops attribute not supported for ebs_block_device with volume_type gp2`),
			},
		},
	})
}

func TestAccEC2Instance_EBSBlockDevice_invalidThroughputForVolumeType(t *testing.T) {
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccInstanceConfigEBSBlockDeviceInvalidThroughput,
				ExpectError: regexp.MustCompile(`error creating resource: throughput attribute not supported for ebs_block_device with volume_type gp2`),
			},
		},
	})
}

// TestAccEC2Instance_EBSBlockDevice_RootBlockDevice_removed verifies block device mappings
// removed outside terraform no longer result in a panic.
// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/20821
func TestAccEC2Instance_EBSBlockDevice_RootBlockDevice_removed(t *testing.T) {
	var instance ec2.Instance
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigEBSAndRootBlockDevice,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance),
					// Instance must be stopped before detaching a root block device
					testAccCheckStopInstance(&instance),
					testAccCheckDetachVolumes(&instance),
				),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccEC2Instance_RootBlockDevice_kmsKeyARN(t *testing.T) {
	var instance ec2.Instance
	kmsKeyResourceName := "aws_kms_key.test"
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigRootBlockDeviceKmsKeyArn(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.encrypted", "true"),
					resource.TestCheckResourceAttrPair(resourceName, "root_block_device.0.kms_key_id", kmsKeyResourceName, "arn"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_userDataBase64(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigWithUserDataBase64(rName, "hello world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "user_data_base64", "aGVsbG8gd29ybGQ="),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"user_data"},
			},
		},
	})
}

func TestAccEC2Instance_userDataBase64_updateWithBashFile(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigWithUserDataBase64(rName, "hello world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "user_data_base64", "aGVsbG8gd29ybGQ="),
				),
			},
			{
				Config: testAccInstanceConfigWithUserDataBase64_Base64EncodedFile(rName, "test-fixtures/userdata-test.sh"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"user_data"},
			},
		},
	})
}

func TestAccEC2Instance_userDataBase64_updateWithZipFile(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigWithUserDataBase64(rName, "hello world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "user_data_base64", "aGVsbG8gd29ybGQ="),
				),
			},
			{
				Config: testAccInstanceConfigWithUserDataBase64_Base64EncodedFile(rName, "test-fixtures/userdata-test.zip"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"user_data"},
			},
		},
	})
}

func TestAccEC2Instance_userDataBase64_update(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigWithUserDataBase64(rName, "hello world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "user_data_base64", "aGVsbG8gd29ybGQ="),
				),
			},
			{
				Config: testAccInstanceConfigWithUserDataBase64(rName, "new world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "user_data_base64", "bmV3IHdvcmxk"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"user_data"},
			},
		},
	})
}

func TestAccEC2Instance_gp2IopsDevice(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"

	testCheck := func() resource.TestCheckFunc {
		return func(*terraform.State) error {
			// Map out the block devices by name, which should be unique.
			blockDevices := make(map[string]*ec2.InstanceBlockDeviceMapping)
			for _, blockDevice := range v.BlockDeviceMappings {
				blockDevices[*blockDevice.DeviceName] = blockDevice
			}

			// Check if the root block device exists.
			if _, ok := blockDevices["/dev/xvda"]; !ok {
				return fmt.Errorf("block device doesn't exist: /dev/xvda")
			}

			return nil
		}
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceGP2IopsDevice(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", "11"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", "gp2"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.iops", "100"),
					testCheck(),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccEC2Instance_gp2WithIopsValue updated in v3.0.0
// to account for apply-time validation of the root_block_device.iops attribute for supported volume types
// Reference: https://github.com/hashicorp/terraform-provider-aws/pull/14310
func TestAccEC2Instance_gp2WithIopsValue(t *testing.T) {
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccInstanceGP2WithIopsValue(),
				ExpectError: regexp.MustCompile(`error creating resource: iops attribute not supported for root_block_device with volume_type gp2`),
			},
		},
	})
}

func TestAccEC2Instance_blockDevices(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"

	testCheck := func() resource.TestCheckFunc {
		return func(*terraform.State) error {
			// Map out the block devices by name, which should be unique.
			blockDevices := make(map[string]*ec2.InstanceBlockDeviceMapping)
			for _, blockDevice := range v.BlockDeviceMappings {
				blockDevices[*blockDevice.DeviceName] = blockDevice
			}

			// Check if the root block device exists.
			if _, ok := blockDevices["/dev/xvda"]; !ok {
				return fmt.Errorf("block device doesn't exist: /dev/xvda")
			}

			// Check if the secondary block device exists.
			if _, ok := blockDevices["/dev/sdb"]; !ok {
				return fmt.Errorf("block device doesn't exist: /dev/sdb")
			}

			// Check if the third block device exists.
			if _, ok := blockDevices["/dev/sdc"]; !ok {
				return fmt.Errorf("block device doesn't exist: /dev/sdc")
			}

			// Check if the encrypted block device exists
			if _, ok := blockDevices["/dev/sdd"]; !ok {
				return fmt.Errorf("block device doesn't exist: /dev/sdd")
			}

			if _, ok := blockDevices["/dev/sdf"]; !ok {
				return fmt.Errorf("block device doesn't exist: /dev/sdf")
			}

			if _, ok := blockDevices["/dev/sdg"]; !ok {
				return fmt.Errorf("block device doesn't exist: /dev/sdg")
			}

			return nil
		}
	}

	rootVolumeSize := "11"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceBlockDevicesConfig(rootVolumeSize),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.#", "1"),
					resource.TestMatchResourceAttr(resourceName, "root_block_device.0.volume_id", regexp.MustCompile("vol-[a-z0-9]+")),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", rootVolumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", "gp2"),
					resource.TestCheckResourceAttr(resourceName, "ebs_block_device.#", "5"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"device_name": "/dev/sdb",
						"volume_size": "9",
						"volume_type": "gp2",
					}),
					resource.TestMatchTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]*regexp.Regexp{
						"volume_id": regexp.MustCompile("vol-[a-z0-9]+"),
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"device_name": "/dev/sdc",
						"volume_size": "10",
						"volume_type": "io1",
						"iops":        "100",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"device_name": "/dev/sdf",
						"volume_size": "10",
						"volume_type": "gp3",
						"throughput":  "300",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"device_name": "/dev/sdg",
						"volume_size": "10",
						"volume_type": "gp3",
						"throughput":  "300",
						"iops":        "4000",
					}),
					resource.TestMatchTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]*regexp.Regexp{
						"volume_id": regexp.MustCompile("vol-[a-z0-9]+"),
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"device_name": "/dev/sdd",
						"encrypted":   "true",
						"volume_size": "12",
					}),
					resource.TestMatchTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]*regexp.Regexp{
						"volume_id": regexp.MustCompile("vol-[a-z0-9]+"),
					}),
					resource.TestCheckResourceAttr(resourceName, "ephemeral_block_device.#", "1"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ephemeral_block_device.*", map[string]string{
						"device_name":  "/dev/sde",
						"virtual_name": "ephemeral0",
					}),
					testCheck(),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"ephemeral_block_device"},
			},
		},
	})
}

func TestAccEC2Instance_rootInstanceStore(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigRootInstanceStore(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "ebs_block_device.#", "0"),
					resource.TestCheckResourceAttr(resourceName, "ebs_optimized", "false"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.#", "0"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_noAMIEphemeralDevices(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"

	testCheck := func() resource.TestCheckFunc {
		return func(*terraform.State) error {
			// Map out the block devices by name, which should be unique.
			blockDevices := make(map[string]*ec2.InstanceBlockDeviceMapping)
			for _, blockDevice := range v.BlockDeviceMappings {
				blockDevices[*blockDevice.DeviceName] = blockDevice
			}

			// Check if the root block device exists.
			if _, ok := blockDevices["/dev/xvda"]; !ok {
				return fmt.Errorf("block device doesn't exist: /dev/xvda")
			}

			// Check if the secondary block not exists.
			if _, ok := blockDevices["/dev/sdb"]; ok {
				return fmt.Errorf("block device exist: /dev/sdb")
			}

			// Check if the third block device not exists.
			if _, ok := blockDevices["/dev/sdc"]; ok {
				return fmt.Errorf("block device exist: /dev/sdc")
			}
			return nil
		}
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigNoAMIEphemeralDevices(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "ebs_optimized", "false"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", "11"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", "gp2"),
					resource.TestCheckResourceAttr(resourceName, "ebs_block_device.#", "0"),
					resource.TestCheckResourceAttr(resourceName, "ephemeral_block_device.#", "2"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ephemeral_block_device.*", map[string]string{
						"device_name": "/dev/sdb",
						"no_device":   "true",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ephemeral_block_device.*", map[string]string{
						"device_name": "/dev/sdc",
						"no_device":   "true",
					}),
					testCheck(),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"ephemeral_block_device"},
			},
		},
	})
}

func TestAccEC2Instance_sourceDestCheck(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	testCheck := func(enabled bool) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if v.SourceDestCheck == nil {
				return fmt.Errorf("bad source_dest_check: got nil")
			}
			if *v.SourceDestCheck != enabled {
				return fmt.Errorf("bad source_dest_check: %#v", *v.SourceDestCheck)
			}

			return nil
		}
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigSourceDestDisable(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					testCheck(false),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfigSourceDestEnable(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					testCheck(true),
				),
			},
			{
				Config: testAccInstanceConfigSourceDestDisable(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					testCheck(false),
				),
			},
		},
	})
}

func TestAccEC2Instance_disableAPITermination(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	checkDisableApiTermination := func(expected bool) resource.TestCheckFunc {
		return func(*terraform.State) error {
			conn := acctest.Provider.Meta().(*conns.AWSClient).EC2Conn
			r, err := conn.DescribeInstanceAttribute(&ec2.DescribeInstanceAttributeInput{
				InstanceId: v.InstanceId,
				Attribute:  aws.String("disableApiTermination"),
			})
			if err != nil {
				return err
			}
			got := *r.DisableApiTermination.Value
			if got != expected {
				return fmt.Errorf("expected: %t, got: %t", expected, got)
			}
			return nil
		}
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigDisableAPITermination(rName, true),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					checkDisableApiTermination(true),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfigDisableAPITermination(rName, false),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					checkDisableApiTermination(false),
				),
			},
		},
	})
}

func TestAccEC2Instance_dedicatedInstance(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccEc2InstanceConfigDedicatedInstance(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "tenancy", "dedicated"),
					resource.TestCheckResourceAttr(resourceName, "user_data", "562a3e32810edf6ff09994f050f12e799452379d"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"associate_public_ip_address", "user_data"},
			},
		},
	})
}

func TestAccEC2Instance_outpost(t *testing.T) {
	var v ec2.Instance
	outpostDataSourceName := "data.aws_outposts_outpost.test"
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t); acctest.PreCheckOutpostsOutposts(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigOutpost(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttrPair(resourceName, "outpost_arn", outpostDataSourceName, "arn"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_placementGroup(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigPlacementGroup(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "placement_group", rName),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_placementPartitionNumber(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigPlacementPartitionNumber(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "placement_group", rName),
					resource.TestCheckResourceAttr(resourceName, "placement_partition_number", "3"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_IPv6_supportAddressCount(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigIpv6Support(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "ipv6_address_count", "1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_ipv6AddressCountAndSingleAddressCausesError(t *testing.T) {
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccInstanceConfigIpv6ErrorConfig(rName),
				ExpectError: regexp.MustCompile("Only 1 of `ipv6_address_count` or `ipv6_addresses` can be specified"),
			},
		},
	})
}

func TestAccEC2Instance_IPv6_supportAddressCountWithIPv4(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigIpv6SupportWithIpv4(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "ipv6_address_count", "1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_networkInstanceSecurityGroups(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceNetworkInstanceSecurityGroups(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_networkInstanceRemovingAllSecurityGroups(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceNetworkInstanceVPCSecurityGroupIDs(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "security_groups.#", "0"),
					resource.TestCheckResourceAttr(resourceName, "vpc_security_group_ids.#", "1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceNetworkInstanceVPCRemoveSecurityGroupIDs(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "security_groups.#", "0"),
					resource.TestCheckResourceAttr(resourceName, "vpc_security_group_ids.#", "1"),
				),
				ExpectError: regexp.MustCompile(`VPC-based instances require at least one security group to be attached`),
			},
		},
	})
}

func TestAccEC2Instance_networkInstanceVPCSecurityGroupIDs(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceNetworkInstanceVPCSecurityGroupIDs(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "security_groups.#", "0"),
					resource.TestCheckResourceAttr(resourceName, "vpc_security_group_ids.#", "1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_tags(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigTags(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "1"),
					resource.TestCheckResourceAttr(resourceName, "tags.test", "test2"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfigTagsUpdate(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "tags.%", "1"),
					resource.TestCheckResourceAttr(resourceName, "tags.test2", "test3"),
				),
			},
		},
	})
}

func TestAccEC2Instance_BlockDeviceTags_volumeTags(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigBlockDeviceTagsNoVolumeTags(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckNoResourceAttr(resourceName, "volume_tags"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"ephemeral_block_device"},
			},
			{
				Config: testAccInstanceConfigBlockDeviceTagsVolumeTags(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "volume_tags.%", "1"),
					resource.TestCheckResourceAttr(resourceName, "volume_tags.Name", "acceptance-test-volume-tag"),
				),
			},
			{
				Config: testAccInstanceConfigBlockDeviceTagsVolumeTagsUpdate(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "volume_tags.%", "2"),
					resource.TestCheckResourceAttr(resourceName, "volume_tags.Name", "acceptance-test-volume-tag"),
					resource.TestCheckResourceAttr(resourceName, "volume_tags.Environment", "dev"),
				),
			},
			{
				Config: testAccInstanceConfigBlockDeviceTagsNoVolumeTags(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckNoResourceAttr(resourceName, "volume_tags"),
				),
			},
		},
	})
}

func TestAccEC2Instance_BlockDeviceTags_withAttachedVolume(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	ebsVolumeName := "aws_ebs_volume.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigBlockDeviceTagsAttachedVolumeWithTags(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(ebsVolumeName, "tags.%", "2"),
					resource.TestCheckResourceAttr(ebsVolumeName, "tags.Name", rName),
					resource.TestCheckResourceAttr(ebsVolumeName, "tags.Factum", "PerAsperaAdAstra"),
				),
			},
			{
				//https://github.com/hashicorp/terraform-provider-aws/issues/17074
				Config: testAccInstanceConfigBlockDeviceTagsAttachedVolumeWithTags(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(ebsVolumeName, "tags.%", "2"),
					resource.TestCheckResourceAttr(ebsVolumeName, "tags.Name", rName),
					resource.TestCheckResourceAttr(ebsVolumeName, "tags.Factum", "PerAsperaAdAstra"),
				),
			},
			{
				Config: testAccInstanceConfigBlockDeviceTagsAttachedVolumeWithTagsUpdate(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(ebsVolumeName, "tags.%", "2"),
					resource.TestCheckResourceAttr(ebsVolumeName, "tags.Name", rName),
					resource.TestCheckResourceAttr(ebsVolumeName, "tags.Factum", "VincitQuiSeVincit"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_BlockDeviceTags_ebsAndRoot(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccInstanceConfigBlockDeviceTagsRootTagsConflict(),
				ExpectError: regexp.MustCompile(`"root_block_device\.0\.tags": conflicts with volume_tags`),
			},
			{
				Config:      testAccInstanceConfigBlockDeviceTagsEBSTagsConflict(),
				ExpectError: regexp.MustCompile(`"ebs_block_device\.0\.tags": conflicts with volume_tags`),
			},
			{
				Config: testAccInstanceConfigBlockDeviceTagsEBSTags(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.tags.%", "0"),
					resource.TestCheckResourceAttr(resourceName, "ebs_block_device.0.tags.%", "1"),
					resource.TestCheckResourceAttr(resourceName, "ebs_block_device.0.tags.Name", rName),
					resource.TestCheckResourceAttr(resourceName, "ebs_block_device.1.tags.%", "0"),
				),
			},
			{
				Config: testAccInstanceConfigBlockDeviceTagsEBSAndRootTags(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.tags.%", "2"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.tags.Name", rName),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.tags.Purpose", "test"),
				),
			},
			{
				Config: testAccInstanceConfigBlockDeviceTagsEBSAndRootTagsUpdate(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.tags.%", "2"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.tags.Name", rName),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.tags.Env", "dev"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"ephemeral_block_device"},
			},
		},
	})
}

func TestAccEC2Instance_instanceProfileChange(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))
	rName2 := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	testCheckInstanceProfile := func() resource.TestCheckFunc {
		return func(*terraform.State) error {
			if v.IamInstanceProfile == nil {
				return fmt.Errorf("Instance Profile is nil - we expected an InstanceProfile associated with the Instance")
			}

			return nil
		}
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigWithoutInstanceProfile(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfigWithInstanceProfile(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					testCheckInstanceProfile(),
				),
			},
			{
				Config: testAccInstanceConfigWithInstanceProfile(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckStopInstance(&v), // GH-8262: Error on EC2 instance role change when stopped
				),
			},
			{
				Config: testAccInstanceConfigWithInstanceProfile(rName2),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					testCheckInstanceProfile(),
				),
			},
		},
	})
}

func TestAccEC2Instance_withIAMInstanceProfile(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	testCheckInstanceProfile := func() resource.TestCheckFunc {
		return func(*terraform.State) error {
			if v.IamInstanceProfile == nil {
				return fmt.Errorf("Instance Profile is nil - we expected an InstanceProfile associated with the Instance")
			}

			return nil
		}
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigWithInstanceProfile(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					testCheckInstanceProfile(),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/17719
func TestAccEC2Instance_withIAMInstanceProfilePath(t *testing.T) {
	var instance ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigWithInstanceProfilePath(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_privateIP(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	testCheckPrivateIP := func() resource.TestCheckFunc {
		return func(*terraform.State) error {
			if *v.PrivateIpAddress != "10.1.1.42" {
				return fmt.Errorf("bad private IP: %s", *v.PrivateIpAddress)
			}

			return nil
		}
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigPrivateIP(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					testCheckPrivateIP(),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_associatePublicIPAndPrivateIP(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	testCheckPrivateIP := func() resource.TestCheckFunc {
		return func(*terraform.State) error {
			if *v.PrivateIpAddress != "10.1.1.42" {
				return fmt.Errorf("bad private IP: %s", *v.PrivateIpAddress)
			}

			return nil
		}
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigAssociatePublicIPAndPrivateIP(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					testCheckPrivateIP(),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Allow Empty Private IP
// https://github.com/hashicorp/terraform-provider-aws/issues/13626
func TestAccEC2Instance_Empty_privateIP(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	testCheckPrivateIP := func() resource.TestCheckFunc {
		return func(*terraform.State) error {
			if aws.StringValue(v.PrivateIpAddress) == "" {
				return fmt.Errorf("bad computed private IP: %s", aws.StringValue(v.PrivateIpAddress))
			}

			return nil
		}
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigEmptyPrivateIP(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					testCheckPrivateIP(),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Guard against regression with KeyPairs
// https://github.com/hashicorp/terraform/issues/2302
func TestAccEC2Instance_keyPairCheck(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	keyPairResourceName := "aws_key_pair.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	publicKey, _, err := sdkacctest.RandSSHKeyPair(acctest.DefaultEmailAddress)
	if err != nil {
		t.Fatalf("error generating random SSH key: %s", err)
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigKeyPair(rName, publicKey),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttrPair(resourceName, "key_name", keyPairResourceName, "key_name"),
				),
			},
		},
	})
}

func TestAccEC2Instance_rootBlockDeviceMismatch(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t); acctest.PreCheckRegion(t, endpoints.UsWest2RegionID) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigRootBlockDeviceMismatch(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", "13"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"root_block_device"},
			},
		},
	})
}

// This test reproduces the bug here:
//   https://github.com/hashicorp/terraform/issues/1752
//
// I wish there were a way to exercise resources built with helper.Schema in a
// unit context, in which case this test could be moved there, but for now this
// will cover the bugfix.
//
// The following triggers "diffs didn't match during apply" without the fix in to
// set NewRemoved on the .# field when it changes to 0.
func TestAccEC2Instance_forceNewAndTagsDrift(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigForceNewAndTagsDrift(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					driftTags(&v),
				),
				ExpectNonEmptyPlan: true,
			},
			{
				Config: testAccInstanceConfigForceNewAndTagsDrift_Update(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_changeInstanceType(t *testing.T) {
	var before ec2.Instance
	var after ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigWithSmallInstanceType(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &before),
					resource.TestCheckResourceAttr(resourceName, "instance_type", "t2.medium"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfigUpdateInstanceType(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &after),
					testAccCheckInstanceNotRecreated(&before, &after),
					resource.TestCheckResourceAttr(resourceName, "instance_type", "t2.large"),
				),
			},
		},
	})
}

func TestAccEC2Instance_changeInstanceTypeAndUserData(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	hash := sha1.Sum([]byte("hello world"))
	expectedUserData := hex.EncodeToString(hash[:])
	hash2 := sha1.Sum([]byte("new world"))
	expectedUserDataUpdated := hex.EncodeToString(hash2[:])

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigInstanceTypeAndUserData(rName, "t2.medium", "hello world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "instance_type", "t2.medium"),
					resource.TestCheckResourceAttr(resourceName, "user_data", expectedUserData),
				),
			},
			{
				Config: testAccInstanceConfigInstanceTypeAndUserData(rName, "t2.large", "new world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "instance_type", "t2.large"),
					resource.TestCheckResourceAttr(resourceName, "user_data", expectedUserDataUpdated),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"user_data"},
			},
		},
	})
}

func TestAccEC2Instance_changeInstanceTypeAndUserDataBase64(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigInstanceTypeAndUserDataBase64(rName, "t2.medium", "hello world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "instance_type", "t2.medium"),
					resource.TestCheckResourceAttr(resourceName, "user_data_base64", "aGVsbG8gd29ybGQ="),
				),
			},
			{
				Config: testAccInstanceConfigInstanceTypeAndUserDataBase64(rName, "t2.large", "new world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "instance_type", "t2.large"),
					resource.TestCheckResourceAttr(resourceName, "user_data_base64", "bmV3IHdvcmxk"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"user_data"},
			},
		},
	})
}

func TestAccEC2Instance_EBSRootDevice_basic(t *testing.T) {
	var instance ec2.Instance
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceEBSRootDeviceBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.#", "1"),
					resource.TestCheckResourceAttrSet(resourceName, "root_block_device.0.delete_on_termination"),
					resource.TestCheckResourceAttrSet(resourceName, "root_block_device.0.encrypted"),
					resource.TestCheckResourceAttrSet(resourceName, "root_block_device.0.iops"),
					resource.TestCheckResourceAttrSet(resourceName, "root_block_device.0.volume_size"),
					resource.TestCheckResourceAttrSet(resourceName, "root_block_device.0.volume_type"),
					resource.TestCheckResourceAttrSet(resourceName, "root_block_device.0.volume_id"),
					resource.TestCheckResourceAttrSet(resourceName, "root_block_device.0.device_name"),
				),
			},
		},
	})
}

func TestAccEC2Instance_EBSRootDevice_modifySize(t *testing.T) {
	var original ec2.Instance
	var updated ec2.Instance
	resourceName := "aws_instance.test"

	deleteOnTermination := "true"
	volumeType := "gp2"

	originalSize := "30"
	updatedSize := "32"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceRootBlockDevice(originalSize, deleteOnTermination, volumeType),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &original),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", originalSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", deleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", volumeType),
				),
			},
			{
				Config: testAccInstanceRootBlockDevice(updatedSize, deleteOnTermination, volumeType),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &updated),
					testAccCheckInstanceNotRecreated(&original, &updated),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", updatedSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", deleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", volumeType),
				),
			},
		},
	})
}

func TestAccEC2Instance_EBSRootDevice_modifyType(t *testing.T) {
	var original ec2.Instance
	var updated ec2.Instance
	resourceName := "aws_instance.test"

	volumeSize := "30"
	deleteOnTermination := "true"

	originalType := "gp2"
	updatedType := "standard"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceRootBlockDevice(volumeSize, deleteOnTermination, originalType),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &original),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", volumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", deleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", originalType),
				),
			},
			{
				Config: testAccInstanceRootBlockDevice(volumeSize, deleteOnTermination, updatedType),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &updated),
					testAccCheckInstanceNotRecreated(&original, &updated),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", volumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", deleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", updatedType),
				),
			},
		},
	})
}

func TestAccEC2Instance_EBSRootDeviceModifyIOPS_io1(t *testing.T) {
	var original ec2.Instance
	var updated ec2.Instance
	resourceName := "aws_instance.test"

	volumeSize := "30"
	deleteOnTermination := "true"
	volumeType := "io1"

	originalIOPS := "100"
	updatedIOPS := "200"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceRootBlockDeviceWithIOPS(volumeSize, deleteOnTermination, volumeType, originalIOPS),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &original),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", volumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", deleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", volumeType),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.iops", originalIOPS),
				),
			},
			{
				Config: testAccInstanceRootBlockDeviceWithIOPS(volumeSize, deleteOnTermination, volumeType, updatedIOPS),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &updated),
					testAccCheckInstanceNotRecreated(&original, &updated),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", volumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", deleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", volumeType),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.iops", updatedIOPS),
				),
			},
		},
	})
}

func TestAccEC2Instance_EBSRootDeviceModifyIOPS_io2(t *testing.T) {
	var original ec2.Instance
	var updated ec2.Instance
	resourceName := "aws_instance.test"

	volumeSize := "30"
	deleteOnTermination := "true"
	volumeType := "io2"

	originalIOPS := "100"
	updatedIOPS := "200"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceRootBlockDeviceWithIOPS(volumeSize, deleteOnTermination, volumeType, originalIOPS),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &original),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", volumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", deleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", volumeType),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.iops", originalIOPS),
				),
			},
			{
				Config: testAccInstanceRootBlockDeviceWithIOPS(volumeSize, deleteOnTermination, volumeType, updatedIOPS),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &updated),
					testAccCheckInstanceNotRecreated(&original, &updated),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", volumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", deleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", volumeType),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.iops", updatedIOPS),
				),
			},
		},
	})
}

func TestAccEC2Instance_EBSRootDeviceModifyThroughput_gp3(t *testing.T) {
	var original ec2.Instance
	var updated ec2.Instance
	resourceName := "aws_instance.test"

	volumeSize := "30"
	deleteOnTermination := "true"
	volumeType := "gp3"

	originalThroughput := "250"
	updatedThroughput := "300"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceRootBlockDeviceWithThroughput(volumeSize, deleteOnTermination, volumeType, originalThroughput),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &original),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", volumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", deleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", volumeType),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.throughput", originalThroughput),
				),
			},
			{
				Config: testAccInstanceRootBlockDeviceWithThroughput(volumeSize, deleteOnTermination, volumeType, updatedThroughput),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &updated),
					testAccCheckInstanceNotRecreated(&original, &updated),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", volumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", deleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", volumeType),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.throughput", updatedThroughput),
				),
			},
		},
	})
}

func TestAccEC2Instance_EBSRootDevice_modifyDeleteOnTermination(t *testing.T) {
	var original ec2.Instance
	var updated ec2.Instance
	resourceName := "aws_instance.test"

	volumeSize := "30"
	volumeType := "gp2"

	originalDeleteOnTermination := "false"
	updatedDeleteOnTermination := "true"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceRootBlockDevice(volumeSize, originalDeleteOnTermination, volumeType),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &original),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", volumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", originalDeleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", volumeType),
				),
			},
			{
				Config: testAccInstanceRootBlockDevice(volumeSize, updatedDeleteOnTermination, volumeType),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &updated),
					testAccCheckInstanceNotRecreated(&original, &updated),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", volumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", updatedDeleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", volumeType),
				),
			},
		},
	})
}

func TestAccEC2Instance_EBSRootDevice_modifyAll(t *testing.T) {
	var original ec2.Instance
	var updated ec2.Instance
	resourceName := "aws_instance.test"

	originalSize := "30"
	updatedSize := "32"

	originalType := "gp2"
	updatedType := "io1"

	updatedIOPS := "200"

	originalDeleteOnTermination := "false"
	updatedDeleteOnTermination := "true"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceRootBlockDevice(originalSize, originalDeleteOnTermination, originalType),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &original),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", originalSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", originalDeleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", originalType),
				),
			},
			{
				Config: testAccInstanceRootBlockDeviceWithIOPS(updatedSize, updatedDeleteOnTermination, updatedType, updatedIOPS),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &updated),
					testAccCheckInstanceNotRecreated(&original, &updated),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", updatedSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", updatedDeleteOnTermination),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", updatedType),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.iops", updatedIOPS),
				),
			},
		},
	})
}

func TestAccEC2Instance_EBSRootDeviceMultipleBlockDevices_modifySize(t *testing.T) {
	var before ec2.Instance
	var after ec2.Instance
	resourceName := "aws_instance.test"

	deleteOnTermination := "true"

	originalRootVolumeSize := "10"
	updatedRootVolumeSize := "14"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceBlockDevicesWithDeleteOnTerminateConfig(originalRootVolumeSize, deleteOnTermination),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &before),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", originalRootVolumeSize),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "9",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "10",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "12",
					}),
				),
			},
			{
				Config: testAccInstanceBlockDevicesWithDeleteOnTerminateConfig(updatedRootVolumeSize, deleteOnTermination),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &after),
					testAccCheckInstanceNotRecreated(&before, &after),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", updatedRootVolumeSize),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "9",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "10",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "12",
					}),
				),
			},
		},
	})
}

func TestAccEC2Instance_EBSRootDeviceMultipleBlockDevices_modifyDeleteOnTermination(t *testing.T) {
	var before ec2.Instance
	var after ec2.Instance
	resourceName := "aws_instance.test"

	rootVolumeSize := "10"

	originalDeleteOnTermination := "false"
	updatedDeleteOnTermination := "true"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceBlockDevicesWithDeleteOnTerminateConfig(rootVolumeSize, originalDeleteOnTermination),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &before),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", rootVolumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", originalDeleteOnTermination),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "9",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "10",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "12",
					}),
				),
			},
			{
				Config: testAccInstanceBlockDevicesWithDeleteOnTerminateConfig(rootVolumeSize, updatedDeleteOnTermination),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &after),
					testAccCheckInstanceNotRecreated(&before, &after),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", rootVolumeSize),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.delete_on_termination", updatedDeleteOnTermination),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "9",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "10",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"volume_size": "12",
					}),
				),
			},
		},
	})
}

// Test to validate fix for GH-ISSUE #1318 (dynamic ebs_block_devices forcing replacement after state refresh)
func TestAccEC2Instance_EBSRootDevice_multipleDynamicEBSBlockDevices(t *testing.T) {
	var instance ec2.Instance

	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceDynamicEBSBlockDevicesConfig(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance),
					resource.TestCheckResourceAttr(resourceName, "ebs_block_device.#", "3"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"delete_on_termination": "true",
						"device_name":           "/dev/sdc",
						"encrypted":             "false",
						"iops":                  "100",
						"volume_size":           "10",
						"volume_type":           "gp2",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"delete_on_termination": "true",
						"device_name":           "/dev/sdb",
						"encrypted":             "false",
						"iops":                  "100",
						"volume_size":           "10",
						"volume_type":           "gp2",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "ebs_block_device.*", map[string]string{
						"delete_on_termination": "true",
						"device_name":           "/dev/sda",
						"encrypted":             "false",
						"iops":                  "100",
						"volume_size":           "10",
						"volume_type":           "gp2",
					}),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_primaryNetworkInterface(t *testing.T) {
	var instance ec2.Instance
	var eni ec2.NetworkInterface
	resourceName := "aws_instance.test"
	eniResourceName := "aws_network_interface.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigPrimaryNetworkInterface(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance),
					testAccCheckENIExists(eniResourceName, &eni),
					resource.TestCheckResourceAttr(resourceName, "network_interface.#", "1"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"network_interface"},
			},
		},
	})
}

func TestAccEC2Instance_primaryNetworkInterfaceSourceDestCheck(t *testing.T) {
	var instance ec2.Instance
	var eni ec2.NetworkInterface
	resourceName := "aws_instance.test"
	eniResourceName := "aws_network_interface.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigPrimaryNetworkInterfaceSourceDestCheck(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance),
					testAccCheckENIExists(eniResourceName, &eni),
					resource.TestCheckResourceAttr(resourceName, "source_dest_check", "false"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"network_interface"},
			},
		},
	})
}

func TestAccEC2Instance_addSecondaryInterface(t *testing.T) {
	var before ec2.Instance
	var after ec2.Instance
	var eniPrimary ec2.NetworkInterface
	var eniSecondary ec2.NetworkInterface
	resourceName := "aws_instance.test"
	eniPrimaryResourceName := "aws_network_interface.primary"
	eniSecondaryResourceName := "aws_network_interface.secondary"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigAddSecondaryNetworkInterfaceBefore(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &before),
					testAccCheckENIExists(eniPrimaryResourceName, &eniPrimary),
					resource.TestCheckResourceAttr(resourceName, "network_interface.#", "1"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"network_interface"},
			},
			{
				Config: testAccInstanceConfigAddSecondaryNetworkInterfaceAfter(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &after),
					testAccCheckENIExists(eniSecondaryResourceName, &eniSecondary),
					resource.TestCheckResourceAttr(resourceName, "network_interface.#", "1"),
				),
			},
		},
	})
}

// https://github.com/hashicorp/terraform/issues/3205
func TestAccEC2Instance_addSecurityGroupNetworkInterface(t *testing.T) {
	var before ec2.Instance
	var after ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigAddSecurityGroupBefore(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &before),
					resource.TestCheckResourceAttr(resourceName, "vpc_security_group_ids.#", "1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfigAddSecurityGroupAfter(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &after),
					resource.TestCheckResourceAttr(resourceName, "vpc_security_group_ids.#", "2"),
				),
			},
		},
	})
}

// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/7063
func TestAccEC2Instance_NewNetworkInterface_publicIPAndSecondaryPrivateIPs(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigPublicAndPrivateSecondaryIPs(rName, true),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "associate_public_ip_address", "true"),
					resource.TestCheckResourceAttrSet(resourceName, "public_ip"),
					resource.TestCheckResourceAttr(resourceName, "secondary_private_ips.#", "2"),
				),
			},
			{
				Config: testAccInstanceConfigPublicAndPrivateSecondaryIPs(rName, false),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "associate_public_ip_address", "false"),
					resource.TestCheckResourceAttr(resourceName, "public_ip", ""),
					resource.TestCheckResourceAttr(resourceName, "secondary_private_ips.#", "2"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/7063
func TestAccEC2Instance_NewNetworkInterface_emptyPrivateIPAndSecondaryPrivateIPs(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)
	secondaryIPs := fmt.Sprintf("%q, %q", "10.1.1.42", "10.1.1.43")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigPrivateIPAndSecondaryIPsNullPrivate(rName, secondaryIPs),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttrSet(resourceName, "private_ip"),
					resource.TestCheckResourceAttr(resourceName, "secondary_private_ips.#", "2"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/7063
func TestAccEC2Instance_NewNetworkInterface_emptyPrivateIPAndSecondaryPrivateIPsUpdate(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)
	secondaryIP := fmt.Sprintf("%q", "10.1.1.42")
	secondaryIPs := fmt.Sprintf("%s, %q", secondaryIP, "10.1.1.43")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigPrivateIPAndSecondaryIPsNullPrivate(rName, secondaryIPs),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttrSet(resourceName, "private_ip"),
					resource.TestCheckResourceAttr(resourceName, "secondary_private_ips.#", "2"),
				),
			},
			{
				Config: testAccInstanceConfigPrivateIPAndSecondaryIPsNullPrivate(rName, ""),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttrSet(resourceName, "private_ip"),
					resource.TestCheckResourceAttr(resourceName, "secondary_private_ips.#", "0"),
				),
			},
			{
				Config: testAccInstanceConfigPrivateIPAndSecondaryIPsNullPrivate(rName, secondaryIP),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttrSet(resourceName, "private_ip"),
					resource.TestCheckResourceAttr(resourceName, "secondary_private_ips.#", "1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/7063
func TestAccEC2Instance_NewNetworkInterface_privateIPAndSecondaryPrivateIPs(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)
	privateIP := "10.1.1.42"
	secondaryIPs := fmt.Sprintf("%q, %q", "10.1.1.43", "10.1.1.44")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigPrivateIPAndSecondaryIPs(rName, privateIP, secondaryIPs),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "private_ip", privateIP),
					resource.TestCheckResourceAttr(resourceName, "secondary_private_ips.#", "2"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/7063
func TestAccEC2Instance_NewNetworkInterface_privateIPAndSecondaryPrivateIPsUpdate(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)
	privateIP := "10.1.1.42"
	secondaryIP := fmt.Sprintf("%q", "10.1.1.43")
	secondaryIPs := fmt.Sprintf("%s, %q", secondaryIP, "10.1.1.44")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigPrivateIPAndSecondaryIPs(rName, privateIP, secondaryIPs),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "private_ip", privateIP),
					resource.TestCheckResourceAttr(resourceName, "secondary_private_ips.#", "2"),
				),
			},
			{
				Config: testAccInstanceConfigPrivateIPAndSecondaryIPs(rName, privateIP, ""),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttrSet(resourceName, "private_ip"),
					resource.TestCheckResourceAttr(resourceName, "secondary_private_ips.#", "0"),
				),
			},
			{
				Config: testAccInstanceConfigPrivateIPAndSecondaryIPs(rName, privateIP, secondaryIP),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "private_ip", privateIP),
					resource.TestCheckResourceAttr(resourceName, "secondary_private_ips.#", "1"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// https://github.com/hashicorp/terraform-provider-aws/issues/227
func TestAccEC2Instance_AssociatePublic_defaultPrivate(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_associatePublic_defaultPrivate(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "associate_public_ip_address", "false"),
					resource.TestCheckResourceAttr(resourceName, "public_ip", ""),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// https://github.com/hashicorp/terraform-provider-aws/issues/227
func TestAccEC2Instance_AssociatePublic_defaultPublic(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_associatePublic_defaultPublic(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "associate_public_ip_address", "true"),
					resource.TestCheckResourceAttrSet(resourceName, "public_ip"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// https://github.com/hashicorp/terraform-provider-aws/issues/227
func TestAccEC2Instance_AssociatePublic_explicitPublic(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_associatePublic_explicitPublic(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "associate_public_ip_address", "true"),
					resource.TestCheckResourceAttrSet(resourceName, "public_ip"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// https://github.com/hashicorp/terraform-provider-aws/issues/227
func TestAccEC2Instance_AssociatePublic_explicitPrivate(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_associatePublic_explicitPrivate(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "associate_public_ip_address", "false"),
					resource.TestCheckResourceAttr(resourceName, "public_ip", ""),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// https://github.com/hashicorp/terraform-provider-aws/issues/227
func TestAccEC2Instance_AssociatePublic_overridePublic(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_associatePublic_overridePublic(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "associate_public_ip_address", "true"),
					resource.TestCheckResourceAttrSet(resourceName, "public_ip"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// https://github.com/hashicorp/terraform-provider-aws/issues/227
func TestAccEC2Instance_AssociatePublic_overridePrivate(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_associatePublic_overridePrivate(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "associate_public_ip_address", "false"),
					resource.TestCheckResourceAttr(resourceName, "public_ip", ""),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_LaunchTemplate_basic(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	launchTemplateResourceName := "aws_launch_template.test"
	amiDataSourceName := "data.aws_ami.amzn-ami-minimal-hvm-ebs"
	instanceTypeDataSourceName := "data.aws_ec2_instance_type_offering.available"

	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_WithTemplate_Basic(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.id", launchTemplateResourceName, "id"),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.name", launchTemplateResourceName, "name"),
					resource.TestCheckResourceAttr(resourceName, "launch_template.0.version", "$Default"),
					resource.TestCheckResourceAttrPair(resourceName, "ami", amiDataSourceName, "id"),
					resource.TestCheckResourceAttrPair(resourceName, "instance_type", instanceTypeDataSourceName, "instance_type"),
				),
			},
		},
	})
}

func TestAccEC2Instance_LaunchTemplate_overrideTemplate(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	launchTemplateResourceName := "aws_launch_template.test"
	amiDataSourceName := "data.aws_ami.amzn-ami-minimal-hvm-ebs"
	instanceTypeDataSourceName := "data.aws_ec2_instance_type_offering.small"

	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_WithTemplate_OverrideTemplate(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.id", launchTemplateResourceName, "id"),
					resource.TestCheckResourceAttrPair(resourceName, "ami", amiDataSourceName, "id"),
					resource.TestCheckResourceAttrPair(resourceName, "instance_type", instanceTypeDataSourceName, "instance_type"),
				),
			},
		},
	})
}

func TestAccEC2Instance_LaunchTemplate_setSpecificVersion(t *testing.T) {
	var v1, v2 ec2.Instance
	resourceName := "aws_instance.test"
	launchTemplateResourceName := "aws_launch_template.test"

	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_WithTemplate_Basic(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v1),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.id", launchTemplateResourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "launch_template.0.version", "$Default"),
				),
			},
			{
				Config: testAccInstanceConfig_WithTemplate_SpecificVersion(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v2),
					testAccCheckInstanceNotRecreated(&v1, &v2),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.id", launchTemplateResourceName, "id"),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.version", launchTemplateResourceName, "default_version"),
				),
			},
		},
	})
}

func TestAccEC2Instance_LaunchTemplateModifyTemplate_defaultVersion(t *testing.T) {
	var v1, v2 ec2.Instance
	resourceName := "aws_instance.test"
	launchTemplateResourceName := "aws_launch_template.test"

	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_WithTemplate_Basic(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v1),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.id", launchTemplateResourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "launch_template.0.version", "$Default"),
				),
			},
			{
				Config: testAccInstanceConfig_WithTemplate_ModifyTemplate(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v2),
					testAccCheckInstanceNotRecreated(&v1, &v2),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.id", launchTemplateResourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "launch_template.0.version", "$Default"),
				),
			},
		},
	})
}

func TestAccEC2Instance_LaunchTemplate_updateTemplateVersion(t *testing.T) {
	var v1, v2 ec2.Instance
	resourceName := "aws_instance.test"
	launchTemplateResourceName := "aws_launch_template.test"

	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_WithTemplate_SpecificVersion(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v1),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.id", launchTemplateResourceName, "id"),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.version", launchTemplateResourceName, "default_version"),
				),
			},
			{
				Config: testAccInstanceConfig_WithTemplate_UpdateVersion(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v2),
					testAccCheckInstanceRecreated(&v1, &v2),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.id", launchTemplateResourceName, "id"),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.version", launchTemplateResourceName, "default_version"),
				),
			},
		},
	})
}

func TestAccEC2Instance_LaunchTemplate_swapIDAndName(t *testing.T) {
	var v1, v2 ec2.Instance
	resourceName := "aws_instance.test"
	launchTemplateResourceName := "aws_launch_template.test"

	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_WithTemplate_Basic(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v1),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.id", launchTemplateResourceName, "id"),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.name", launchTemplateResourceName, "name"),
				),
			},
			{
				Config: testAccInstanceConfig_WithTemplate_WithName(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v2),
					testAccCheckInstanceNotRecreated(&v1, &v2),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.id", launchTemplateResourceName, "id"),
					resource.TestCheckResourceAttrPair(resourceName, "launch_template.0.name", launchTemplateResourceName, "name"),
				),
			},
		},
	})
}

func TestAccEC2Instance_GetPasswordData_falseToTrue(t *testing.T) {
	var before, after ec2.Instance
	resourceName := "aws_instance.test"

	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))
	publicKey, _, err := sdkacctest.RandSSHKeyPair(acctest.DefaultEmailAddress)
	if err != nil {
		t.Fatalf("error generating random SSH key: %s", err)
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_getPasswordData(rName, publicKey, false),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &before),
					resource.TestCheckResourceAttr(resourceName, "get_password_data", "false"),
					resource.TestCheckResourceAttr(resourceName, "password_data", ""),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig_getPasswordData(rName, publicKey, true),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &after),
					testAccCheckInstanceNotRecreated(&before, &after),
					resource.TestCheckResourceAttr(resourceName, "get_password_data", "true"),
					resource.TestCheckResourceAttrSet(resourceName, "password_data"),
				),
			},
		},
	})
}

func TestAccEC2Instance_GetPasswordData_trueToFalse(t *testing.T) {
	var before, after ec2.Instance
	resourceName := "aws_instance.test"

	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))
	publicKey, _, err := sdkacctest.RandSSHKeyPair(acctest.DefaultEmailAddress)
	if err != nil {
		t.Fatalf("error generating random SSH key: %s", err)
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_getPasswordData(rName, publicKey, true),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &before),
					resource.TestCheckResourceAttr(resourceName, "get_password_data", "true"),
					resource.TestCheckResourceAttrSet(resourceName, "password_data"),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password_data", "get_password_data"},
			},
			{
				Config: testAccInstanceConfig_getPasswordData(rName, publicKey, false),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &after),
					testAccCheckInstanceNotRecreated(&before, &after),
					resource.TestCheckResourceAttr(resourceName, "get_password_data", "false"),
					resource.TestCheckResourceAttr(resourceName, "password_data", ""),
				),
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecificationEmpty_nonBurstable(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_CreditSpecification_Empty_NonBurstable(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"credit_specification"},
			},
		},
	})
}

// Reference: https://github.com/hashicorp/terraform-provider-aws/issues/10203
func TestAccEC2Instance_CreditSpecificationUnspecifiedToEmpty_nonBurstable(t *testing.T) {
	var instance ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_CreditSpecification_Unspecified_NonBurstable(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig_CreditSpecification_Empty_NonBurstable(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance),
				),
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecification_unspecifiedDefaultsToStandard(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_unspecified(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecification_standardCPUCredits(t *testing.T) {
	var first, second ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_standardCpuCredits(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &first),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig_creditSpecification_unspecified(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &second),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecification_unlimitedCPUCredits(t *testing.T) {
	var first, second ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_unlimitedCpuCredits(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &first),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "unlimited"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig_creditSpecification_unspecified(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &second),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "unlimited"),
				),
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecificationUnknownCPUCredits_t2(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_unknownCpuCredits(rName, "t2.micro"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecificationUnknownCPUCredits_t3(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_unknownCpuCredits(rName, "t3.micro"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "unlimited"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecification_updateCPUCredits(t *testing.T) {
	var first, second, third ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_standardCpuCredits(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &first),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig_creditSpecification_unlimitedCpuCredits(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &second),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "unlimited"),
				),
			},
			{
				Config: testAccInstanceConfig_creditSpecification_standardCpuCredits(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &third),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecification_isNotAppliedToNonBurstable(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_isNotAppliedToNonBurstable(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"credit_specification"},
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecificationT3_unspecifiedDefaultsToUnlimited(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_unspecified_t3(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "unlimited"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecificationT3_standardCPUCredits(t *testing.T) {
	var first, second ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_standardCpuCredits_t3(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &first),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig_creditSpecification_unspecified_t3(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &second),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecificationT3_unlimitedCPUCredits(t *testing.T) {
	var first, second ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_unlimitedCpuCredits_t3(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &first),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "unlimited"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig_creditSpecification_unspecified_t3(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &second),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "unlimited"),
				),
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecificationT3_updateCPUCredits(t *testing.T) {
	var first, second, third ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_standardCpuCredits_t3(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &first),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig_creditSpecification_unlimitedCpuCredits_t3(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &second),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "unlimited"),
				),
			},
			{
				Config: testAccInstanceConfig_creditSpecification_standardCpuCredits_t3(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &third),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecificationStandardCPUCredits_t2Tot3Taint(t *testing.T) {
	var before, after ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_standardCpuCredits(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &before),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig_creditSpecification_standardCpuCredits_t3(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &after),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "standard"),
				),
				Taint: []string{resourceName},
			},
		},
	})
}

func TestAccEC2Instance_CreditSpecificationUnlimitedCPUCredits_t2Tot3Taint(t *testing.T) {
	var before, after ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_creditSpecification_unlimitedCpuCredits(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &before),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "unlimited"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfig_creditSpecification_unlimitedCpuCredits_t3(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &after),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "credit_specification.0.cpu_credits", "unlimited"),
				),
				Taint: []string{resourceName},
			},
		},
	})
}

func TestAccEC2Instance_disappears(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigBasic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					acctest.CheckResourceDisappears(acctest.Provider, tfec2.ResourceInstance(), resourceName),
				),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccEC2Instance_UserData(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigWithUserData(rName, "hello world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"user_data"},
			},
		},
	})
}

func TestAccEC2Instance_UserData_update(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigWithUserData(rName, "hello world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				Config: testAccInstanceConfigWithUserData(rName, "new world"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"user_data"},
			},
		},
	})
}

func TestAccEC2Instance_UserData_emptyStringToUnspecified(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_UserData_EmptyString(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:            resourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"user_data"},
			},
			// Switching should show no difference
			{
				Config:             testAccInstanceConfig_UserData_Unspecified(rName),
				ExpectNonEmptyPlan: false,
				PlanOnly:           true,
			},
		},
	})
}

func TestAccEC2Instance_UserData_unspecifiedToEmptyString(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := fmt.Sprintf("tf-testacc-instance-%s", sdkacctest.RandString(12))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_UserData_Unspecified(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Switching should show no difference
			{
				Config:             testAccInstanceConfig_UserData_EmptyString(rName),
				ExpectNonEmptyPlan: false,
				PlanOnly:           true,
			},
		},
	})
}

func TestAccEC2Instance_hibernation(t *testing.T) {
	var instance1, instance2 ec2.Instance
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigHibernation(true),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance1),
					resource.TestCheckResourceAttr(resourceName, "hibernation", "true"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfigHibernation(false),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance2),
					testAccCheckInstanceRecreated(&instance1, &instance2),
					resource.TestCheckResourceAttr(resourceName, "hibernation", "false"),
				),
			},
		},
	})
}

func TestAccEC2Instance_metadataOptions(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigMetadataOptions(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "metadata_options.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "metadata_options.0.http_endpoint", "disabled"),
					resource.TestCheckResourceAttr(resourceName, "metadata_options.0.http_tokens", "optional"),
					resource.TestCheckResourceAttr(resourceName, "metadata_options.0.http_put_response_hop_limit", "1"),
					resource.TestCheckResourceAttr(resourceName, "metadata_options.0.instance_metadata_tags", "disabled"),
				),
			},
			{
				Config: testAccInstanceConfigMetadataOptionsUpdated(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "metadata_options.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "metadata_options.0.http_endpoint", "enabled"),
					resource.TestCheckResourceAttr(resourceName, "metadata_options.0.http_tokens", "required"),
					resource.TestCheckResourceAttr(resourceName, "metadata_options.0.http_put_response_hop_limit", "2"),
					resource.TestCheckResourceAttr(resourceName, "metadata_options.0.instance_metadata_tags", "enabled"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_enclaveOptions(t *testing.T) {
	var instance1, instance2 ec2.Instance
	resourceName := "aws_instance.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigEnclaveOptions(true),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance1),
					resource.TestCheckResourceAttr(resourceName, "enclave_options.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "enclave_options.0.enabled", "true"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccInstanceConfigEnclaveOptions(false),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &instance2),
					testAccCheckInstanceRecreated(&instance1, &instance2),
					resource.TestCheckResourceAttr(resourceName, "enclave_options.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "enclave_options.0.enabled", "false"),
				),
			},
		},
	})
}

func TestAccEC2Instance_CapacityReservation_unspecifiedDefaultsToOpen(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigCapacityReservationSpecification_unspecified(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.0.capacity_reservation_preference", "open"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Adding 'open' preference should show no difference
			{
				Config:             testAccInstanceConfigCapacityReservationSpecification_preference(rName, "open"),
				ExpectNonEmptyPlan: false,
				PlanOnly:           true,
			},
		},
	})
}

func TestAccEC2Instance_CapacityReservationPreference_open(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigCapacityReservationSpecification_preference(rName, "open"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.0.capacity_reservation_preference", "open"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_CapacityReservationPreference_none(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigCapacityReservationSpecification_preference(rName, "none"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.0.capacity_reservation_preference", "none"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_CapacityReservation_targetID(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigCapacityReservationSpecification_targetId(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.0.capacity_reservation_target.#", "1"),
					resource.TestCheckResourceAttrSet(resourceName, "capacity_reservation_specification.0.capacity_reservation_target.0.capacity_reservation_id"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEC2Instance_CapacityReservation_modifyPreference(t *testing.T) {
	var original ec2.Instance
	var updated ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigCapacityReservationSpecification_preference(rName, "open"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &original),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.0.capacity_reservation_preference", "open"),
				),
			},
			{Config: testAccInstanceConfigCapacityReservationSpecification_preference(rName, "open"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckStopInstance(&original), // Stop instance to modify capacity reservation
				),
			},
			{
				Config: testAccInstanceConfigCapacityReservationSpecification_preference(rName, "none"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &updated),
					testAccCheckInstanceNotRecreated(&original, &updated),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.0.capacity_reservation_preference", "none"),
				),
			},
		},
	})
}

func TestAccEC2Instance_CapacityReservation_modifyTarget(t *testing.T) {
	var original ec2.Instance
	var updated ec2.Instance
	resourceName := "aws_instance.test"
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigCapacityReservationSpecification_preference(rName, "none"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &original),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.0.capacity_reservation_preference", "none"),
				),
			},
			{Config: testAccInstanceConfigCapacityReservationSpecification_preference(rName, "none"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckStopInstance(&original), // Stop instance to modify capacity reservation
				),
			},
			{
				Config: testAccInstanceConfigCapacityReservationSpecification_targetId(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &updated),
					testAccCheckInstanceNotRecreated(&original, &updated),
					resource.TestCheckResourceAttr(resourceName, "capacity_reservation_specification.0.capacity_reservation_target.#", "1"),
					resource.TestCheckResourceAttrSet(resourceName, "capacity_reservation_specification.0.capacity_reservation_target.0.capacity_reservation_id"),
				),
			},
		},
	})
}

func testAccCheckInstanceNotRecreated(before, after *ec2.Instance) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if before, after := aws.StringValue(before.InstanceId), aws.StringValue(after.InstanceId); before != after {
			return fmt.Errorf("EC2 Instance (%s/%s) recreated", before, after)
		}

		return nil
	}
}

func testAccCheckInstanceRecreated(before, after *ec2.Instance) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if before, after := aws.StringValue(before.InstanceId), aws.StringValue(after.InstanceId); before == after {
			return fmt.Errorf("EC2 Instance (%s) not recreated", before)
		}

		return nil
	}
}

func testAccCheckInstanceDestroy(s *terraform.State) error {
	return testAccCheckInstanceDestroyWithProvider(s, acctest.Provider)
}

func testAccCheckInstanceDestroyWithProvider(s *terraform.State, provider *schema.Provider) error {
	conn := provider.Meta().(*conns.AWSClient).EC2Conn

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "aws_instance" {
			continue
		}

		// Try to find the resource
		instance, err := tfec2.InstanceFindByID(conn, rs.Primary.ID)
		if err == nil {
			if instance.State != nil && *instance.State.Name != "terminated" {
				return fmt.Errorf("Found unterminated instance: %s", rs.Primary.ID)
			}
		}

		// Verify the error is what we want
		if tfawserr.ErrMessageContains(err, "InvalidInstanceID.NotFound", "") {
			continue
		}

		return err
	}

	return nil
}

func testAccCheckInstanceExists(n string, i *ec2.Instance) resource.TestCheckFunc {
	return testAccCheckInstanceExistsWithProvider(n, i, func() *schema.Provider { return acctest.Provider })
}

func testAccCheckInstanceEc2ClassicExists(n string, i *ec2.Instance) resource.TestCheckFunc {
	return testAccCheckInstanceExistsWithProvider(n, i, func() *schema.Provider { return acctest.ProviderEC2Classic })
}

func testAccCheckInstanceExistsWithProvider(n string, i *ec2.Instance, providerF func() *schema.Provider) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		provider := providerF()

		conn := provider.Meta().(*conns.AWSClient).EC2Conn
		instance, err := tfec2.InstanceFindByID(conn, rs.Primary.ID)
		if err != nil {
			return err
		}

		if instance != nil {
			*i = *instance
			return nil
		}

		return fmt.Errorf("Instance not found")
	}
}

func testAccCheckStopInstance(instance *ec2.Instance) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		conn := acctest.Provider.Meta().(*conns.AWSClient).EC2Conn

		params := &ec2.StopInstancesInput{
			InstanceIds: []*string{instance.InstanceId},
		}
		if _, err := conn.StopInstances(params); err != nil {
			return err
		}

		return tfec2.WaitForInstanceStopping(conn, *instance.InstanceId, 10*time.Minute)
	}
}

func testAccCheckDetachVolumes(instance *ec2.Instance) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := acctest.Provider.Meta().(*conns.AWSClient)
		conn := client.EC2Conn

		for _, bd := range instance.BlockDeviceMappings {
			if bd.Ebs != nil && bd.Ebs.VolumeId != nil {
				name := aws.StringValue(bd.DeviceName)
				volID := aws.StringValue(bd.Ebs.VolumeId)
				instanceID := aws.StringValue(instance.InstanceId)

				// Make sure in correct state before detaching
				if err := tfec2.WaitVolumeAttachmentAttached(conn, name, volID, instanceID); err != nil {
					return err
				}

				r := tfec2.ResourceVolumeAttachment()
				d := r.Data(nil)
				d.Set("device_name", name)
				d.Set("volume_id", volID)
				d.Set("instance_id", instanceID)

				if err := r.Delete(d, client); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

func TestInstanceHostIDSchema(t *testing.T) {
	actualSchema := tfec2.ResourceInstance().Schema["host_id"]
	expectedSchema := &schema.Schema{
		Type:     schema.TypeString,
		Optional: true,
		Computed: true,
		ForceNew: true,
	}
	if !reflect.DeepEqual(actualSchema, expectedSchema) {
		t.Fatalf(
			"Got:\n\n%#v\n\nExpected:\n\n%#v\n",
			actualSchema,
			expectedSchema)
	}
}

func TestInstanceCpuCoreCountSchema(t *testing.T) {
	actualSchema := tfec2.ResourceInstance().Schema["cpu_core_count"]
	expectedSchema := &schema.Schema{
		Type:     schema.TypeInt,
		Optional: true,
		Computed: true,
		ForceNew: true,
	}
	if !reflect.DeepEqual(actualSchema, expectedSchema) {
		t.Fatalf(
			"Got:\n\n%#v\n\nExpected:\n\n%#v\n",
			actualSchema,
			expectedSchema)
	}
}

func TestInstanceCpuThreadsPerCoreSchema(t *testing.T) {
	actualSchema := tfec2.ResourceInstance().Schema["cpu_threads_per_core"]
	expectedSchema := &schema.Schema{
		Type:     schema.TypeInt,
		Optional: true,
		Computed: true,
		ForceNew: true,
	}
	if !reflect.DeepEqual(actualSchema, expectedSchema) {
		t.Fatalf(
			"Got:\n\n%#v\n\nExpected:\n\n%#v\n",
			actualSchema,
			expectedSchema)
	}
}

func driftTags(instance *ec2.Instance) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		conn := acctest.Provider.Meta().(*conns.AWSClient).EC2Conn
		_, err := conn.CreateTags(&ec2.CreateTagsInput{
			Resources: []*string{instance.InstanceId},
			Tags: []*ec2.Tag{
				{
					Key:   aws.String("Drift"),
					Value: aws.String("Happens"),
				},
			},
		})
		return err
	}
}

// testAccPreCheckEC2ClassicOrHasDefaultVPCWithDefaultSubnets checks that the test region has either
// - The EC2-Classic platform available, or
// - A default VPC with default subnets.
// This check is useful to ensure that an instance can be launched without specifying a subnet.
func testAccPreCheckEC2ClassicOrHasDefaultVPCWithDefaultSubnets(t *testing.T) {
	client := acctest.Provider.Meta().(*conns.AWSClient)

	if !conns.HasEC2Classic(client.SupportedPlatforms) && !(hasDefaultVPC(t) && defaultSubnetCount(t) > 0) {
		t.Skipf("skipping tests; %s does not have EC2-Classic or a default VPC with default subnets", client.Region)
	}
}

// defaultVPC returns the ID of the default VPC for the current AWS Region, or "" if none exists.
func defaultVPC(t *testing.T) string {
	conn := acctest.Provider.Meta().(*conns.AWSClient).EC2Conn

	output, err := conn.DescribeAccountAttributes(&ec2.DescribeAccountAttributesInput{
		AttributeNames: aws.StringSlice([]string{ec2.AccountAttributeNameDefaultVpc}),
	})

	if acctest.PreCheckSkipError(err) {
		return ""
	}

	if err != nil {
		t.Fatalf("error describing EC2 account attributes: %s", err)
	}

	if len(output.AccountAttributes) > 0 && len(output.AccountAttributes[0].AttributeValues) > 0 {
		if v := aws.StringValue(output.AccountAttributes[0].AttributeValues[0].AttributeValue); v != "none" {
			return v
		}
	}

	return ""
}

func hasDefaultVPC(t *testing.T) bool {
	return defaultVPC(t) != ""
}

// defaultSubnetCount returns the number of default subnets in the current region's default VPC.
func defaultSubnetCount(t *testing.T) int {
	conn := acctest.Provider.Meta().(*conns.AWSClient).EC2Conn

	input := &ec2.DescribeSubnetsInput{
		Filters: tfec2.BuildAttributeFilterList(
			map[string]string{
				"defaultForAz": "true",
			},
		),
	}

	subnets, err := tfec2.FindSubnets(conn, input)

	if acctest.PreCheckSkipError(err) {
		return 0
	}

	if err != nil {
		t.Fatalf("error listing default subnets: %s", err)
	}

	return len(subnets)
}

func testAccAvailableAZsWavelengthZonesExcludeConfig(excludeZoneIds ...string) string {
	return fmt.Sprintf(`
data "aws_availability_zones" "available" {
  exclude_zone_ids = [%[1]q]
  state            = "available"

  filter {
    name   = "zone-type"
    values = ["wavelength-zone"]
  }

  filter {
    name   = "opt-in-status"
    values = ["opted-in"]
  }
}
`, strings.Join(excludeZoneIds, "\", \""))
}

func testAccAvailableAZsWavelengthZonesDefaultExcludeConfig() string {
	// Exclude usw2-wl1-den-wlz1 as there may be problems allocating carrier IP addresses.
	return testAccAvailableAZsWavelengthZonesExcludeConfig("usw2-wl1-den-wlz1")
}

func testAccInstanceConfigInDefaultVpcBySgName(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigAvailableAZsNoOptInDefaultExclude(),
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		fmt.Sprintf(`
data "aws_vpc" "default" {
  default = true
}

resource "aws_security_group" "test" {
  name        = %[1]q
  description = %[1]q
  vpc_id      = data.aws_vpc.default.id
}

resource "aws_instance" "test" {
  ami               = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type     = "t2.micro"
  security_groups   = [aws_security_group.test.name]
  availability_zone = data.aws_availability_zones.available.names[0]
}
`, rName))
}

func testAccInstanceConfigInDefaultVpcBySgId(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigAvailableAZsNoOptInDefaultExclude(),
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		fmt.Sprintf(`
data "aws_vpc" "default" {
  default = true
}

resource "aws_security_group" "test" {
  name        = %[1]q
  description = %[1]q
  vpc_id      = data.aws_vpc.default.id
}

resource "aws_instance" "test" {
  ami                    = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type          = "t2.micro"
  vpc_security_group_ids = [aws_security_group.test.id]
  availability_zone      = data.aws_availability_zones.available.names[0]
}
`, rName))
}

func testAccInstanceConfigInEc2Classic() string {
	return acctest.ConfigCompose(
		acctest.ConfigEC2ClassicRegionProvider(),
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegion("t1.micro", "m3.medium", "m3.large", "c3.large", "r3.large"),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
}
`)
}

func testAccInstanceConfigBasic() string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-classic-platform.html#ec2-classic-instance-types
		acctest.AvailableEC2InstanceTypeForRegion("t3.micro", "t2.micro", "t1.micro", "m1.small"),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
  # Explicitly no tags so as to test creation without tags.
}
`)
}

func testAccInstanceConfigAtLeastOneOtherEbsVolume(rName string) string {
	return acctest.ConfigCompose(
		testAccLatestAmazonLinuxHVMInstanceStoreAMIConfig(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
# Ensure that there is at least 1 EBS volume in the current region.
# See https://github.com/hashicorp/terraform/issues/1249.
resource "aws_ebs_volume" "test" {
  availability_zone = data.aws_availability_zones.available.names[0]
  size              = 5

  tags = {
    Name = %[1]q
  }
}

resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-instance-store.id

  # tflint-ignore: aws_instance_previous_type
  instance_type = "m1.small"
  subnet_id     = aws_subnet.test.id
  user_data     = "foo:-with-character's"

  tags = {
    Name = %[1]q
  }

  depends_on = [aws_ebs_volume.test]
}
`, rName))
}

func testAccInstanceConfigWithUserData(rName, userData string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami       = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  subnet_id = aws_subnet.test.id

  instance_type = "t2.small"
  user_data     = %[1]q
}
`, userData))
}

func testAccInstanceConfigWithUserDataBase64(rName, userData string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami       = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  subnet_id = aws_subnet.test.id

  instance_type    = "t2.small"
  user_data_base64 = base64encode(%[1]q)
}
`, userData))
}

func testAccInstanceConfigWithUserDataBase64_Base64EncodedFile(rName, filename string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami       = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  subnet_id = aws_subnet.test.id

  instance_type    = "t2.small"
  user_data_base64 = filebase64(%[1]q)
}
`, filename))
}

func testAccInstanceConfigWithSmallInstanceType(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami       = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  subnet_id = aws_subnet.test.id

  instance_type = "t2.medium"

  tags = {
    Name = "tf-acctest"
  }
}
`)
}

func testAccInstanceConfigUpdateInstanceType(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami       = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  subnet_id = aws_subnet.test.id

  instance_type = "t2.large"

  tags = {
    Name = "tf-acctest"
  }
}
`)
}

func testAccInstanceConfigInstanceTypeAndUserData(rName, instanceType, userData string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami       = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  subnet_id = aws_subnet.test.id

  instance_type = %[1]q
  user_data     = %[2]q
}
`, instanceType, userData))
}

func testAccInstanceConfigInstanceTypeAndUserDataBase64(rName, instanceType, userData string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami       = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  subnet_id = aws_subnet.test.id

  instance_type    = %[1]q
  user_data_base64 = base64encode(%[2]q)

}
`, instanceType, userData))
}

func testAccInstanceGP2IopsDevice() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"
    volume_size = 11
  }
}
`)
}

func testAccInstanceGP2WithIopsValue() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"
    volume_size = 11
    # configured explicitly
    iops = 10
  }
}
`)
}

func testAccInstanceConfigRootInstanceStore() string {
	return acctest.ConfigCompose(testAccLatestAmazonLinuxHVMInstanceStoreAMIConfig(), `
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-instance-store.id

  # Only certain instance types support ephemeral root instance stores.
  # http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/InstanceStorage.html
  # tflint-ignore: aws_instance_previous_type
  instance_type = "m3.medium"
}
`)
}

func testAccInstanceConfigNoAMIEphemeralDevices() string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegion("t3.micro", "t2.micro"),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type

  root_block_device {
    volume_type = "gp2"
    volume_size = 11
  }

  ephemeral_block_device {
    device_name = "/dev/sdb"
    no_device   = true
  }

  ephemeral_block_device {
    device_name = "/dev/sdc"
    no_device   = true
  }
}
`)
}

func testAccInstanceEBSRootDeviceBasic() string {
	return acctest.ConfigCompose(testAccInstanceAMIWithEBSRootVolume, `
resource "aws_instance" "test" {
  ami = data.aws_ami.ami.id

  instance_type = "t2.medium"
}
`)
}

func testAccInstanceRootBlockDevice(size, delete, volumeType string) string {
	return testAccInstanceRootBlockDeviceWithIOPS(size, delete, volumeType, "")
}

func testAccInstanceRootBlockDeviceWithIOPS(size, delete, volumeType, iops string) string {
	if iops == "" {
		iops = "null"
	}
	return acctest.ConfigCompose(testAccInstanceAMIWithEBSRootVolume, fmt.Sprintf(`
resource "aws_instance" "test" {
  ami = data.aws_ami.ami.id

  instance_type = "t2.medium"

  root_block_device {
    volume_size           = %[1]s
    delete_on_termination = %[2]s
    volume_type           = %[3]q
    iops                  = %[4]s
  }
}
`, size, delete, volumeType, iops))
}

func testAccInstanceRootBlockDeviceWithThroughput(size, delete, volumeType, throughput string) string {
	if throughput == "" {
		throughput = "null"
	}
	return acctest.ConfigCompose(testAccInstanceAMIWithEBSRootVolume, fmt.Sprintf(`
resource "aws_instance" "test" {
  ami = data.aws_ami.ami.id

  instance_type = "t2.medium"

  root_block_device {
    volume_size           = %[1]s
    delete_on_termination = %[2]s
    volume_type           = %[3]q
    throughput            = %[4]s
  }
}
`, size, delete, volumeType, throughput))
}

func TestAccEC2Instance_gp3RootBlockDevice(t *testing.T) {
	var v ec2.Instance
	resourceName := "aws_instance.test"

	testCheck := func() resource.TestCheckFunc {
		return func(*terraform.State) error {
			// Map out the block devices by name, which should be unique.
			blockDevices := make(map[string]*ec2.InstanceBlockDeviceMapping)
			for _, blockDevice := range v.BlockDeviceMappings {
				blockDevices[*blockDevice.DeviceName] = blockDevice
			}

			// Check if the root block device exists.
			if _, ok := blockDevices["/dev/xvda"]; !ok {
				return fmt.Errorf("block device doesn't exist: /dev/xvda")
			}

			return nil
		}
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { acctest.PreCheck(t) },
		ErrorCheck:   acctest.ErrorCheck(t, ec2.EndpointsID),
		Providers:    acctest.Providers,
		CheckDestroy: testAccCheckInstanceDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfigGP3RootBlockDevice(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckInstanceExists(resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_size", "10"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.volume_type", "gp3"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.iops", "4000"),
					resource.TestCheckResourceAttr(resourceName, "root_block_device.0.throughput", "300"),
					testCheck(),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccInstanceConfigGP3RootBlockDevice() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.medium"

  root_block_device {
    volume_size = 10
    volume_type = "gp3"
    throughput  = 300
    iops        = 4000
  }
}
`)
}

const testAccInstanceAMIWithEBSRootVolume = `
data "aws_ami" "ami" {
  owners      = ["amazon"]
  most_recent = true

  filter {
    name   = "name"
    values = ["amzn2-ami-*"]
  }

  filter {
    name   = "root-device-type"
    values = ["ebs"]
  }

  filter {
    name   = "architecture"
    values = ["x86_64"]
  }
}
`

func testAccInstanceBlockDevicesConfig(size string) string {
	return testAccInstanceBlockDevicesWithDeleteOnTerminateConfig(size, "")
}

func testAccInstanceBlockDevicesWithDeleteOnTerminateConfig(size, delete string) string {
	if delete == "" {
		delete = "null"
	}

	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), fmt.Sprintf(`
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type           = "gp2"
    volume_size           = %[1]s
    delete_on_termination = %[2]s
  }

  ebs_block_device {
    device_name = "/dev/sdb"
    volume_size = 9
  }

  ebs_block_device {
    device_name = "/dev/sdc"
    volume_size = 10
    volume_type = "io1"
    iops        = 100
  }

  # Encrypted ebs block device

  ebs_block_device {
    device_name = "/dev/sdd"
    volume_size = 12
    encrypted   = true
  }

  ephemeral_block_device {
    device_name  = "/dev/sde"
    virtual_name = "ephemeral0"
  }

  ebs_block_device {
    device_name = "/dev/sdf"
    volume_size = 10
    volume_type = "gp3"
    throughput  = 300
  }

  ebs_block_device {
    device_name = "/dev/sdg"
    volume_size = 10
    volume_type = "gp3"
    throughput  = 300
    iops        = 4000
  }
}
`, size, delete))
}

func testAccInstanceConfigSourceDestEnable(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.small"
  subnet_id     = aws_subnet.test.id
}
`)
}

func testAccInstanceConfigSourceDestDisable(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami               = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type     = "t2.small"
  subnet_id         = aws_subnet.test.id
  source_dest_check = false
}
`)
}

func testAccInstanceConfigDisableAPITermination(rName string, val bool) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami                     = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type           = "t2.small"
  subnet_id               = aws_subnet.test.id
  disable_api_termination = %[1]t
}
`, val))
}

func testAccEc2InstanceConfigDedicatedInstance(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  # tflint-ignore: aws_instance_previous_type
  instance_type               = "m1.small"
  subnet_id                   = aws_subnet.test.id
  associate_public_ip_address = true
  tenancy                     = "dedicated"
  # pre-encoded base64 data
  user_data = "3dc39dda39be1205215e776bad998da361a5955d"
}
`)
}

func testAccInstanceConfigOutpost() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
data "aws_outposts_outposts" "test" {}

data "aws_outposts_outpost" "test" {
  id = tolist(data.aws_outposts_outposts.test.ids)[0]
}

data "aws_outposts_outpost_instance_types" "test" {
  arn = data.aws_outposts_outpost.test.arn
}

resource "aws_vpc" "test" {
  cidr_block = "10.1.0.0/16"
}

resource "aws_subnet" "test" {
  availability_zone = data.aws_outposts_outpost.test.availability_zone
  cidr_block        = "10.1.1.0/24"
  outpost_arn       = data.aws_outposts_outpost.test.arn
  vpc_id            = aws_vpc.test.id
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = tolist(data.aws_outposts_outpost_instance_types.test.instance_types)[0]
  subnet_id     = aws_subnet.test.id

  root_block_device {
    volume_type = "gp2"
    volume_size = 8
  }
}
`)
}

func testAccInstanceConfigPlacementGroup(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_placement_group" "test" {
  name     = %[1]q
  strategy = "cluster"
}

# Limitations: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/placement-groups.html#concepts-placement-groups
resource "aws_instance" "test" {
  ami                         = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type               = "c5.large"
  subnet_id                   = aws_subnet.test.id
  associate_public_ip_address = true
  placement_group             = aws_placement_group.test.name

  # pre-encoded base64 data
  user_data = "3dc39dda39be1205215e776bad998da361a5955d"

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigPlacementPartitionNumber(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_placement_group" "test" {
  name            = %[1]q
  strategy        = "partition"
  partition_count = 4
}

# Limitations: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/placement-groups.html#concepts-placement-groups
resource "aws_instance" "test" {
  ami                         = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type               = "c5.large"
  subnet_id                   = aws_subnet.test.id
  associate_public_ip_address = true
  placement_group             = aws_placement_group.test.name
  placement_partition_number  = 3

  # pre-encoded base64 data
  user_data = "3dc39dda39be1205215e776bad998da361a5955d"

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigIpv6ErrorConfig(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCIPv6Config(rName),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami                = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type      = "t2.micro"
  subnet_id          = aws_subnet.test.id
  ipv6_addresses     = ["2600:1f14:bb2:e501::10"]
  ipv6_address_count = 1

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigIpv6Support(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCIPv6Config(rName),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami                = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type      = "t2.micro"
  subnet_id          = aws_subnet.test.id
  ipv6_address_count = 1

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigIpv6SupportWithIpv4(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCIPv6Config(rName),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami                         = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type               = "t2.micro"
  subnet_id                   = aws_subnet.test.id
  associate_public_ip_address = true
  ipv6_address_count          = 1

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigTags() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.small"

  tags = {
    test = "test2"
  }
}
`)
}

func testAccInstanceConfigEbsBlockDeviceKmsKeyArn() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_kms_key" "test" {
  deletion_window_in_days = 7
}

resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"
    volume_size = 11
  }

  # Encrypted ebs block device

  ebs_block_device {
    device_name = "/dev/sdd"
    encrypted   = true
    kms_key_id  = aws_kms_key.test.arn
    volume_size = 12
  }
}
`)
}

func testAccInstanceConfigRootBlockDeviceKmsKeyArn(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_kms_key" "test" {
  deletion_window_in_days = 7
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t3.nano"
  subnet_id     = aws_subnet.test.id

  root_block_device {
    delete_on_termination = true
    encrypted             = true
    kms_key_id            = aws_kms_key.test.arn
  }
}
`)
}

func testAccInstanceConfigBlockDeviceTagsAttachedVolumeWithTags(rName string) string {
	// https://github.com/hashicorp/terraform-provider-aws/issues/17074
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.ConfigAvailableAZsNoOptIn(),
		acctest.AvailableEC2InstanceTypeForAvailabilityZone("data.aws_availability_zones.available.names[0]", "t3.micro", "t2.micro"),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami               = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type     = data.aws_ec2_instance_type_offering.available.instance_type
  availability_zone = data.aws_availability_zones.available.names[0]
}

resource "aws_ebs_volume" "test" {
  availability_zone = aws_instance.test.availability_zone
  size              = "10"
  type              = "gp2"

  tags = {
    Name   = %[1]q
    Factum = "PerAsperaAdAstra"
  }
}

resource "aws_volume_attachment" "test" {
  device_name = "/dev/xvdg"
  volume_id   = aws_ebs_volume.test.id
  instance_id = aws_instance.test.id
}
`, rName))
}

func testAccInstanceConfigBlockDeviceTagsAttachedVolumeWithTagsUpdate(rName string) string {
	// https://github.com/hashicorp/terraform-provider-aws/issues/17074
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.ConfigAvailableAZsNoOptIn(),
		acctest.AvailableEC2InstanceTypeForAvailabilityZone("data.aws_availability_zones.available.names[0]", "t3.micro", "t2.micro"),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami               = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type     = data.aws_ec2_instance_type_offering.available.instance_type
  availability_zone = data.aws_availability_zones.available.names[0]
}

resource "aws_ebs_volume" "test" {
  availability_zone = aws_instance.test.availability_zone
  size              = "10"
  type              = "gp2"

  tags = {
    Name   = %[1]q
    Factum = "VincitQuiSeVincit"
  }
}

resource "aws_volume_attachment" "test" {
  device_name = "/dev/xvdg"
  volume_id   = aws_ebs_volume.test.id
  instance_id = aws_instance.test.id
}
`, rName))
}

func testAccInstanceConfigBlockDeviceTagsRootTagsConflict() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"
    volume_size = 11

    tags = {
      Name = "root-tag"
    }
  }

  ebs_block_device {
    device_name = "/dev/sdb"
    volume_size = 9
  }

  volume_tags = {
    Name = "volume-tags"
  }
}
`)
}

func testAccInstanceConfigBlockDeviceTagsEBSTagsConflict() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"
    volume_size = 11
  }

  ebs_block_device {
    device_name = "/dev/sdb"
    volume_size = 9

    tags = {
      Name = "ebs-volume"
    }
  }

  volume_tags = {
    Name = "volume-tags"
  }
}
`)
}

func testAccInstanceConfigBlockDeviceTagsNoVolumeTags() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"
    volume_size = 11
  }

  ebs_block_device {
    device_name = "/dev/sdb"
    volume_size = 9
  }

  ebs_block_device {
    device_name = "/dev/sdc"
    volume_size = 10
    volume_type = "io1"
    iops        = 100
  }

  ebs_block_device {
    device_name = "/dev/sdd"
    volume_size = 12
    encrypted   = true
  }

  ephemeral_block_device {
    device_name  = "/dev/sde"
    virtual_name = "ephemeral0"
  }
}
`)
}

func testAccInstanceConfigBlockDeviceTagsEBSTags(rName string) string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), fmt.Sprintf(`
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"
  }

  ebs_block_device {
    device_name = "/dev/sdb"
    volume_size = 1

    tags = {
      Name = %[1]q
    }
  }

  ebs_block_device {
    device_name = "/dev/sdc"
    volume_size = 1
  }

  ephemeral_block_device {
    device_name  = "/dev/sde"
    virtual_name = "ephemeral0"
  }
}
`, rName))
}

func testAccInstanceConfigBlockDeviceTagsEBSAndRootTags(rName string) string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), fmt.Sprintf(`
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"

    tags = {
      Name    = %[1]q
      Purpose = "test"
    }
  }

  ebs_block_device {
    device_name = "/dev/sdb"
    volume_size = 1

    tags = {
      Name = %[1]q
    }
  }

  ebs_block_device {
    device_name = "/dev/sdc"
    volume_size = 1
  }

  ephemeral_block_device {
    device_name  = "/dev/sde"
    virtual_name = "ephemeral0"
  }
}
`, rName))
}

func testAccInstanceConfigBlockDeviceTagsEBSAndRootTagsUpdate(rName string) string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), fmt.Sprintf(`
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"

    tags = {
      Name = %[1]q
      Env  = "dev"
    }
  }

  ebs_block_device {
    device_name = "/dev/sdb"
    volume_size = 1

    tags = {
      Name = %[1]q
    }
  }

  ebs_block_device {
    device_name = "/dev/sdc"
    volume_size = 1
  }

  ephemeral_block_device {
    device_name  = "/dev/sde"
    virtual_name = "ephemeral0"
  }
}
`, rName))
}

var testAccInstanceConfigEBSBlockDeviceInvalidIops = acctest.ConfigCompose(testAccInstanceAMIWithEBSRootVolume, `
resource "aws_instance" "test" {
  ami = data.aws_ami.ami.id

  instance_type = "t2.medium"

  ebs_block_device {
    device_name = "/dev/sdc"
    volume_size = 10
    volume_type = "gp2"
    iops        = 100
  }
}
`)

var testAccInstanceConfigEBSBlockDeviceInvalidThroughput = acctest.ConfigCompose(testAccInstanceAMIWithEBSRootVolume, `
resource "aws_instance" "test" {
  ami = data.aws_ami.ami.id

  instance_type = "t2.medium"

  ebs_block_device {
    device_name = "/dev/sdc"
    volume_size = 10
    volume_type = "gp2"
    throughput  = 300
  }
}
`)

var testAccInstanceConfigEBSAndRootBlockDevice = acctest.ConfigCompose(
	acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
	`
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type           = "gp2"
    volume_size           = 9
    delete_on_termination = true
  }

  ebs_block_device {
    device_name = "/dev/sdb"
    volume_size = 9
  }
}
`)

func testAccInstanceConfigBlockDeviceTagsVolumeTags() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"
    volume_size = 11
  }

  ebs_block_device {
    device_name = "/dev/sdb"
    volume_size = 9
  }

  ebs_block_device {
    device_name = "/dev/sdc"
    volume_size = 10
    volume_type = "io1"
    iops        = 100
  }

  ebs_block_device {
    device_name = "/dev/sdd"
    volume_size = 12
    encrypted   = true
  }

  ephemeral_block_device {
    device_name  = "/dev/sde"
    virtual_name = "ephemeral0"
  }

  volume_tags = {
    Name = "acceptance-test-volume-tag"
  }
}
`)
}

func testAccInstanceConfigBlockDeviceTagsVolumeTagsUpdate() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-hvm-ebs.id

  instance_type = "t2.medium"

  root_block_device {
    volume_type = "gp2"
    volume_size = 11
  }

  ebs_block_device {
    device_name = "/dev/sdb"
    volume_size = 9
  }

  ebs_block_device {
    device_name = "/dev/sdc"
    volume_size = 10
    volume_type = "io1"
    iops        = 100
  }

  ebs_block_device {
    device_name = "/dev/sdd"
    volume_size = 12
    encrypted   = true
  }

  ephemeral_block_device {
    device_name  = "/dev/sde"
    virtual_name = "ephemeral0"
  }

  volume_tags = {
    Name        = "acceptance-test-volume-tag"
    Environment = "dev"
  }
}
`)
}

func testAccInstanceConfigTagsUpdate() string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), `
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.small"

  tags = {
    test2 = "test3"
  }
}
`)
}

func testAccInstanceConfigWithoutInstanceProfile(rName string) string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), fmt.Sprintf(`
resource "aws_iam_role" "test" {
  name = %[1]q

  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": [
          "ec2.amazonaws.com"
        ]
      },
      "Action": [
        "sts:AssumeRole"
      ]
    }
  ]
}
EOF
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.small"

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigWithInstanceProfile(rName string) string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), fmt.Sprintf(`
resource "aws_iam_role" "test" {
  name = %[1]q

  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": [
          "ec2.amazonaws.com"
        ]
      },
      "Action": [
        "sts:AssumeRole"
      ]
    }
  ]
}
EOF
}

resource "aws_iam_instance_profile" "test" {
  name = %[1]q
  role = aws_iam_role.test.name
}

resource "aws_instance" "test" {
  ami                  = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type        = "t2.small"
  iam_instance_profile = aws_iam_instance_profile.test.name

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigWithInstanceProfilePath(rName string) string {
	return acctest.ConfigCompose(acctest.ConfigLatestAmazonLinuxHvmEbsAmi(), fmt.Sprintf(`
data "aws_partition" "current" {}

resource "aws_iam_role" "test" {
  name = %[1]q

  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": [
          "ec2.${data.aws_partition.current.dns_suffix}"
        ]
      },
      "Action": [
        "sts:AssumeRole"
      ]
    }
  ]
}
EOF
}

resource "aws_iam_instance_profile" "test" {
  name = %[1]q
  path = "/test/"
  role = aws_iam_role.test.name
}

resource "aws_instance" "test" {
  ami                  = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type        = "t2.small"
  iam_instance_profile = aws_iam_instance_profile.test.name

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigPrivateIP(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id
  private_ip    = "10.1.1.42"
}
`)
}

func testAccInstanceConfigEmptyPrivateIP(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id
  private_ip    = null
}
`)
}

func testAccInstanceConfigAssociatePublicIPAndPrivateIP(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami                         = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type               = "t2.micro"
  subnet_id                   = aws_subnet.test.id
  associate_public_ip_address = true
  private_ip                  = "10.1.1.42"
}
`)
}

func testAccInstanceNetworkInstanceSecurityGroups(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		testAccInstanceVPCSecurityGroupConfig(rName),
		`
resource "aws_instance" "test" {
  ami                         = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type               = "t2.micro"
  vpc_security_group_ids      = [aws_security_group.test.id]
  subnet_id                   = aws_subnet.test.id
  associate_public_ip_address = true
  depends_on                  = [aws_internet_gateway.test]
}

resource "aws_eip" "test" {
  instance   = aws_instance.test.id
  vpc        = true
  depends_on = [aws_internet_gateway.test]
}
`)
}

func testAccInstanceNetworkInstanceVPCSecurityGroupIDs(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		testAccInstanceVPCSecurityGroupConfig(rName),
		`
resource "aws_instance" "test" {
  ami                    = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type          = "t2.micro"
  vpc_security_group_ids = [aws_security_group.test.id]
  subnet_id              = aws_subnet.test.id
  depends_on             = [aws_internet_gateway.test]
}

resource "aws_eip" "test" {
  instance   = aws_instance.test.id
  vpc        = true
  depends_on = [aws_internet_gateway.test]
}
`)
}

func testAccInstanceNetworkInstanceVPCRemoveSecurityGroupIDs(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		testAccInstanceVPCSecurityGroupConfig(rName),
		`
resource "aws_instance" "test" {
  ami                    = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type          = "t2.micro"
  vpc_security_group_ids = []
  subnet_id              = aws_subnet.test.id
  depends_on             = [aws_internet_gateway.test]
}

resource "aws_eip" "test" {
  instance   = aws_instance.test.id
  vpc        = true
  depends_on = [aws_internet_gateway.test]
}
`)
}

func testAccInstanceConfigKeyPair(rName, publicKey string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		fmt.Sprintf(`
resource "aws_key_pair" "test" {
  key_name   = %[1]q
  public_key = %[2]q
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  key_name      = aws_key_pair.test.key_name

  tags = {
    Name = %[1]q
  }
}
`, rName, publicKey))
}

func testAccInstanceConfigRootBlockDeviceMismatch(rName string) string {
	return acctest.ConfigCompose(
		testAccInstanceVPCConfig(rName, false), `
resource "aws_instance" "test" {
  # This is an AMI in UsWest2 with RootDeviceName: "/dev/sda1"; actual root: "/dev/sda"
  ami = "ami-ef5b69df"

  # tflint-ignore: aws_instance_previous_type
  instance_type = "t1.micro"
  subnet_id     = aws_subnet.test.id

  root_block_device {
    volume_size = 13
  }
}
`) //lintignore:AWSAT002
}

func testAccInstanceConfigForceNewAndTagsDrift(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.nano"
  subnet_id     = aws_subnet.test.id
}
`)
}

func testAccInstanceConfigForceNewAndTagsDrift_Update(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id
}
`)
}

func testAccInstanceConfigPrimaryNetworkInterface(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_network_interface" "test" {
  subnet_id   = aws_subnet.test.id
  private_ips = ["10.1.1.42"]

  tags = {
    Name = %[1]q
  }
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"

  network_interface {
    network_interface_id = aws_network_interface.test.id
    device_index         = 0
  }
}
`, rName))
}

func testAccInstanceConfigPrimaryNetworkInterfaceSourceDestCheck(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_network_interface" "test" {
  subnet_id         = aws_subnet.test.id
  private_ips       = ["10.1.1.42"]
  source_dest_check = false

  tags = {
    Name = %[1]q
  }
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"

  network_interface {
    network_interface_id = aws_network_interface.test.id
    device_index         = 0
  }
}
`, rName))
}

func testAccInstanceConfigAddSecondaryNetworkInterfaceBefore(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_network_interface" "primary" {
  subnet_id   = aws_subnet.test.id
  private_ips = ["10.1.1.42"]

  tags = {
    Name = %[1]q
  }
}

resource "aws_network_interface" "secondary" {
  subnet_id   = aws_subnet.test.id
  private_ips = ["10.1.1.43"]

  tags = {
    Name = %[1]q
  }
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"

  network_interface {
    network_interface_id = aws_network_interface.primary.id
    device_index         = 0
  }
}
`, rName))
}

func testAccInstanceConfigAddSecondaryNetworkInterfaceAfter(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_network_interface" "primary" {
  subnet_id   = aws_subnet.test.id
  private_ips = ["10.1.1.42"]

  tags = {
    Name = %[1]q
  }
}

# Attach previously created network interface, observe no state diff on instance resource
resource "aws_network_interface" "secondary" {
  subnet_id   = aws_subnet.test.id
  private_ips = ["10.1.1.43"]

  tags = {
    Name = %[1]q
  }

  attachment {
    instance     = aws_instance.test.id
    device_index = 1
  }
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"

  network_interface {
    network_interface_id = aws_network_interface.primary.id
    device_index         = 0
  }
}
`, rName))
}

func testAccInstanceConfigAddSecurityGroupBefore(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_subnet" "test2" {
  cidr_block        = "10.1.2.0/24"
  vpc_id            = aws_vpc.test.id
  availability_zone = data.aws_availability_zones.available.names[0]

  tags = {
    Name = %[1]q
  }
}

resource "aws_security_group" "test" {
  vpc_id      = aws_vpc.test.id
  description = "%[1]s_1"
  name        = "%[1]s_1"
}

resource "aws_security_group" "test2" {
  vpc_id      = aws_vpc.test.id
  description = "%[1]s_2"
  name        = "%[1]s_2"
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id

  associate_public_ip_address = false

  vpc_security_group_ids = [
    aws_security_group.test.id,
  ]

  tags = {
    Name = %[1]q
  }
}

resource "aws_network_interface" "test" {
  subnet_id       = aws_subnet.test.id
  private_ips     = ["10.1.1.42"]
  security_groups = [aws_security_group.test.id]

  attachment {
    instance     = aws_instance.test.id
    device_index = 1
  }

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigAddSecurityGroupAfter(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_subnet" "test2" {
  cidr_block        = "10.1.2.0/24"
  vpc_id            = aws_vpc.test.id
  availability_zone = data.aws_availability_zones.available.names[0]

  tags = {
    Name = %[1]q
  }
}

resource "aws_security_group" "test" {
  vpc_id      = aws_vpc.test.id
  description = "%[1]s_1"
  name        = "%[1]s_1"
}

resource "aws_security_group" "test2" {
  vpc_id      = aws_vpc.test.id
  description = "%[1]s_2"
  name        = "%[1]s_2"
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id

  associate_public_ip_address = false

  vpc_security_group_ids = [
    aws_security_group.test.id,
    aws_security_group.test2.id,
  ]

  tags = {
    Name = %[1]q
  }
}

resource "aws_network_interface" "test" {
  subnet_id       = aws_subnet.test.id
  private_ips     = ["10.1.1.42"]
  security_groups = [aws_security_group.test.id]

  attachment {
    instance     = aws_instance.test.id
    device_index = 1
  }

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigPublicAndPrivateSecondaryIPs(rName string, isPublic bool) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_security_group" "test" {
  vpc_id      = aws_vpc.test.id
  description = %[1]q
  name        = %[1]q
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t3.small"
  subnet_id     = aws_subnet.test.id

  associate_public_ip_address = %[2]t

  secondary_private_ips = ["10.1.1.42", "10.1.1.43"]

  vpc_security_group_ids = [
    aws_security_group.test.id
  ]

  tags = {
    Name = %[1]q
  }
}
`, rName, isPublic))
}

func testAccInstanceConfigPrivateIPAndSecondaryIPs(rName, privateIP, secondaryIPs string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_security_group" "test" {
  vpc_id      = aws_vpc.test.id
  description = %[1]q
  name        = %[1]q
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t3.small"
  subnet_id     = aws_subnet.test.id

  private_ip            = %[2]q
  secondary_private_ips = [%[3]s]

  vpc_security_group_ids = [
    aws_security_group.test.id
  ]

  tags = {
    Name = %[1]q
  }
}
`, rName, privateIP, secondaryIPs))
}

func testAccInstanceConfigPrivateIPAndSecondaryIPsNullPrivate(rName, secondaryIPs string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_security_group" "test" {
  vpc_id      = aws_vpc.test.id
  description = %[1]q
  name        = %[1]q
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t3.small"
  subnet_id     = aws_subnet.test.id

  private_ip            = null
  secondary_private_ips = [%[2]s]

  vpc_security_group_ids = [
    aws_security_group.test.id
  ]

  tags = {
    Name = %[1]q
  }
}
`, rName, secondaryIPs))
}

func testAccInstanceConfig_associatePublic_defaultPrivate(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfig_associatePublic_defaultPublic(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, true),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfig_associatePublic_explicitPublic(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, true),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami                         = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type               = "t2.micro"
  subnet_id                   = aws_subnet.test.id
  associate_public_ip_address = true

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfig_associatePublic_explicitPrivate(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, true),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami                         = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type               = "t2.micro"
  subnet_id                   = aws_subnet.test.id
  associate_public_ip_address = false

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfig_associatePublic_overridePublic(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami                         = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type               = "t2.micro"
  subnet_id                   = aws_subnet.test.id
  associate_public_ip_address = true

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfig_associatePublic_overridePrivate(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, true),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami                         = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type               = "t2.micro"
  subnet_id                   = aws_subnet.test.id
  associate_public_ip_address = false

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

// testAccLatestWindowsServer2016CoreAMIConfig returns the configuration for a data source that
// describes the latest Microsoft Windows Server 2016 Core AMI.
// The data source is named 'win2016core-ami'.
func testAccLatestWindowsServer2016CoreAMIConfig() string {
	return `
data "aws_ami" "win2016core-ami" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["Windows_Server-2016-English-Core-Base-*"]
  }
}
`
}

func testAccInstanceConfig_getPasswordData(rName, publicKey string, val bool) string {
	return acctest.ConfigCompose(testAccLatestWindowsServer2016CoreAMIConfig(), fmt.Sprintf(`
resource "aws_key_pair" "test" {
  key_name   = %[1]q
  public_key = %[2]q
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.win2016core-ami.id
  instance_type = "t2.medium"
  key_name      = aws_key_pair.test.key_name

  get_password_data = %[3]t
}
`, rName, publicKey, val))
}

func testAccInstanceConfig_CreditSpecification_Empty_NonBurstable(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "m5.large"
  subnet_id     = aws_subnet.test.id

  credit_specification {}
}
`)
}

func testAccInstanceConfig_CreditSpecification_Unspecified_NonBurstable(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "m5.large"
  subnet_id     = aws_subnet.test.id
}
`)
}

func testAccInstanceConfig_creditSpecification_unspecified(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id
}
`)
}

func testAccInstanceConfig_creditSpecification_unspecified_t3(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t3.micro"
  subnet_id     = aws_subnet.test.id
}
`)
}

func testAccInstanceConfig_creditSpecification_standardCpuCredits(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id

  credit_specification {
    cpu_credits = "standard"
  }
}
`)
}

func testAccInstanceConfig_creditSpecification_standardCpuCredits_t3(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t3.micro"
  subnet_id     = aws_subnet.test.id

  credit_specification {
    cpu_credits = "standard"
  }
}
`)
}

func testAccInstanceConfig_creditSpecification_unlimitedCpuCredits(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id

  credit_specification {
    cpu_credits = "unlimited"
  }
}
`)
}

func testAccInstanceConfig_creditSpecification_unlimitedCpuCredits_t3(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t3.micro"
  subnet_id     = aws_subnet.test.id

  credit_specification {
    cpu_credits = "unlimited"
  }
}
`)
}

func testAccInstanceConfig_creditSpecification_isNotAppliedToNonBurstable(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.small"
  subnet_id     = aws_subnet.test.id

  credit_specification {
    cpu_credits = "standard"
  }
}
`)
}

func testAccInstanceConfig_creditSpecification_unknownCpuCredits(rName, instanceType string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = %[1]q
  subnet_id     = aws_subnet.test.id

  credit_specification {}
}
`, instanceType))
}

func testAccInstanceConfig_UserData_Unspecified(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id
}
`)
}

func testAccInstanceConfig_UserData_EmptyString(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.test.id
  user_data     = ""
}
`)
}

// testAccInstanceVPCConfig returns the configuration for tests that create
//   1) a VPC without IPv6 support
//   2) a subnet in the VPC that optionally assigns public IP addresses to ENIs
// The resources are named 'test'.
func testAccInstanceVPCConfig(rName string, mapPublicIpOnLaunch bool) string {
	return acctest.ConfigCompose(acctest.ConfigAvailableAZsNoOptInDefaultExclude(), fmt.Sprintf(`
resource "aws_vpc" "test" {
  cidr_block = "10.1.0.0/16"

  tags = {
    Name = %[1]q
  }
}

resource "aws_subnet" "test" {
  cidr_block              = "10.1.1.0/24"
  vpc_id                  = aws_vpc.test.id
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = %[2]t

  tags = {
    Name = %[1]q
  }
}
`, rName, mapPublicIpOnLaunch))
}

func testAccInstanceVPCBasicConfig(rName string) string {
	return acctest.ConfigCompose(acctest.ConfigAvailableAZsNoOptIn(), fmt.Sprintf(`
resource "aws_vpc" "test" {
  cidr_block = "10.0.0.0/16"

  tags = {
    Name = %[1]q
  }
}

resource "aws_subnet" "test" {
  availability_zone = data.aws_availability_zones.available.names[0]
  cidr_block        = "10.0.0.0/24"
  vpc_id            = aws_vpc.test.id

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

// testAccInstanceVPCSecurityGroupConfig returns the configuration for tests that create
//   1) a VPC security group
//   2) an internet gateway in the VPC
// The resources are named 'test'.
func testAccInstanceVPCSecurityGroupConfig(rName string) string {
	return fmt.Sprintf(`
resource "aws_internet_gateway" "test" {
  vpc_id = aws_vpc.test.id

  tags = {
    Name = %[1]q
  }
}

resource "aws_security_group" "test" {
  name        = %[1]q
  description = "test"
  vpc_id      = aws_vpc.test.id

  ingress {
    protocol    = "icmp"
    from_port   = -1
    to_port     = -1
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = %[1]q
  }
}
`, rName)
}

// testAccInstanceVPCIPv6Config returns the configuration for tests that create
//   1) a VPC with IPv6 support
//   2) a subnet in the VPC with an assigned IPv6 CIDR block
// The resources are named 'test'.
func testAccInstanceVPCIPv6Config(rName string) string {
	return acctest.ConfigCompose(acctest.ConfigAvailableAZsNoOptInDefaultExclude(), fmt.Sprintf(`
resource "aws_vpc" "test" {
  cidr_block = "10.1.0.0/16"

  assign_generated_ipv6_cidr_block = true

  tags = {
    Name = %[1]q
  }
}

resource "aws_subnet" "test" {
  cidr_block        = "10.1.1.0/24"
  vpc_id            = aws_vpc.test.id
  availability_zone = data.aws_availability_zones.available.names[0]
  ipv6_cidr_block   = cidrsubnet(aws_vpc.test.ipv6_cidr_block, 8, 1)

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigHibernation(hibernation bool) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		fmt.Sprintf(`
resource "aws_vpc" "test" {
  cidr_block = "10.1.0.0/16"

  tags = {
    Name = "terraform-testacc-instance-hibernation"
  }
}

resource "aws_subnet" "test" {
  cidr_block = "10.1.1.0/24"
  vpc_id     = aws_vpc.test.id

  tags = {
    Name = "tf-acc-instance-hibernation"
  }
}

# must be >= m3 and have an encrypted root volume to eanble hibernation
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  hibernation   = %[1]t
  instance_type = "m5.large"
  subnet_id     = aws_subnet.test.id

  root_block_device {
    encrypted   = true
    volume_size = 20
  }
}
`, hibernation))
}

func testAccInstanceConfigMetadataOptions(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		acctest.AvailableEC2InstanceTypeForRegion("t3.micro", "t2.micro"),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
  subnet_id     = aws_subnet.test.id

  tags = {
    Name = %[1]q
  }

  metadata_options {
    http_endpoint = "disabled"
  }
}

data "aws_instance" "test" {
  instance_id = aws_instance.test.id
}
`, rName))
}

func testAccInstanceConfigMetadataOptionsUpdated(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(rName, false),
		acctest.AvailableEC2InstanceTypeForRegion("t3.micro", "t2.micro"),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
  subnet_id     = aws_subnet.test.id

  tags = {
    Name = %[1]q
  }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 2
    instance_metadata_tags      = "enabled"
  }
}
`, rName))
}

func testAccInstanceConfigEnclaveOptions(enabled bool) string {
	name := "tf-acc-instance-enclaves"
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		testAccInstanceVPCConfig(name, false),
		acctest.AvailableEC2InstanceTypeForRegion("c5a.xlarge", "c5.xlarge"),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
  subnet_id     = aws_subnet.test.id

  enclave_options {
    enabled = %[2]t
  }

  tags = {
    Name = %[1]q
  }
}
`, name, enabled))
}

// testAccLatestAmazonLinuxPVEBSAMIConfig returns the configuration for a data source that
// describes the latest Amazon Linux AMI using PV virtualization and an EBS root device.
// The data source is named 'amzn-ami-minimal-pv-ebs'.
func testAccLatestAmazonLinuxPVEBSAMIConfig() string {
	return `
data "aws_ami" "amzn-ami-minimal-pv-ebs" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["amzn-ami-minimal-pv-*"]
  }

  filter {
    name   = "root-device-type"
    values = ["ebs"]
  }
}
`
}

func testAccInstanceDynamicEBSBlockDevicesConfig() string {
	return acctest.ConfigCompose(testAccLatestAmazonLinuxPVEBSAMIConfig(), `
resource "aws_instance" "test" {
  ami = data.aws_ami.amzn-ami-minimal-pv-ebs.id
  # tflint-ignore: aws_instance_previous_type
  instance_type = "m3.medium"

  dynamic "ebs_block_device" {
    for_each = ["a", "b", "c"]
    iterator = device

    content {
      device_name = format("/dev/sd%s", device.value)
      volume_size = "10"
      volume_type = "gp2"
    }
  }
}
`)
}

func testAccInstanceConfigCapacityReservationSpecification_unspecified(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegion("t2.micro"),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type

  tags = {
    Name = %[1]q
  }
}
`, rName))
}

func testAccInstanceConfigCapacityReservationSpecification_preference(rName, crPreference string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegion("t2.micro"),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type

  capacity_reservation_specification {
    capacity_reservation_preference = %[2]q
  }

  tags = {
    Name = %[1]q
  }
}
`, rName, crPreference))
}

func testAccInstanceConfigCapacityReservationSpecification_targetId(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegion("t2.micro"),
		fmt.Sprintf(`
resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type

  capacity_reservation_specification {
    capacity_reservation_target {
      capacity_reservation_id = aws_ec2_capacity_reservation.test.id
    }
  }

  tags = {
    Name = %[1]q
  }
}

data "aws_availability_zones" "available" {
  state = "available"

  filter {
    name   = "opt-in-status"
    values = ["opt-in-not-required"]
  }
}

resource "aws_ec2_capacity_reservation" "test" {
  instance_type     = data.aws_ec2_instance_type_offering.available.instance_type
  instance_platform = %[2]q
  availability_zone = data.aws_availability_zones.available.names[0]
  instance_count    = 10

  tags = {
    Name = %[1]q
  }
}
`, rName, ec2.CapacityReservationInstancePlatformLinuxUnix))
}

func testAccInstanceConfig_WithTemplate_Basic(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegion("t3.micro", "t2.micro", "t1.micro", "m1.small"),
		fmt.Sprintf(`
resource "aws_launch_template" "test" {
  name          = %[1]q
  image_id      = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
}

resource "aws_instance" "test" {
  launch_template {
    id = aws_launch_template.test.id
  }
}
`, rName))
}

func testAccInstanceConfig_WithTemplate_OverrideTemplate(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegionNamed("micro", "t3.micro", "t2.micro", "t1.micro", "m1.small"),
		acctest.AvailableEC2InstanceTypeForRegionNamed("small", "t3.small", "t2.small", "t1.small", "m1.medium"),
		fmt.Sprintf(`
resource "aws_launch_template" "test" {
  name          = %[1]q
  instance_type = data.aws_ec2_instance_type_offering.micro.instance_type
}

resource "aws_instance" "test" {
  ami           = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.small.instance_type

  launch_template {
    id = aws_launch_template.test.id
  }
}
`, rName))
}

func testAccInstanceConfig_WithTemplate_SpecificVersion(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegion("t3.micro", "t2.micro", "t1.micro", "m1.small"),
		fmt.Sprintf(`
resource "aws_launch_template" "test" {
  name          = %[1]q
  image_id      = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
}

resource "aws_instance" "test" {
  launch_template {
    id      = aws_launch_template.test.id
    version = aws_launch_template.test.default_version
  }
}
`, rName))
}

func testAccInstanceConfig_WithTemplate_ModifyTemplate(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegion("t3.small", "t2.small", "t1.small", "m1.medium"),
		fmt.Sprintf(`
resource "aws_launch_template" "test" {
  name          = %[1]q
  image_id      = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
}

resource "aws_instance" "test" {
  launch_template {
    id = aws_launch_template.test.id
  }
}
`, rName))
}

func testAccInstanceConfig_WithTemplate_UpdateVersion(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegion("t3.small", "t2.small", "t1.small", "m1.medium"),
		fmt.Sprintf(`
resource "aws_launch_template" "test" {
  name          = %[1]q
  image_id      = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type

  update_default_version = true
}

resource "aws_instance" "test" {
  launch_template {
    id      = aws_launch_template.test.id
    version = aws_launch_template.test.default_version
  }
}
`, rName))
}

func testAccInstanceConfig_WithTemplate_WithName(rName string) string {
	return acctest.ConfigCompose(
		acctest.ConfigLatestAmazonLinuxHvmEbsAmi(),
		acctest.AvailableEC2InstanceTypeForRegion("t3.micro", "t2.micro", "t1.micro", "m1.small"),
		fmt.Sprintf(`
resource "aws_launch_template" "test" {
  name          = %[1]q
  image_id      = data.aws_ami.amzn-ami-minimal-hvm-ebs.id
  instance_type = data.aws_ec2_instance_type_offering.available.instance_type
}

resource "aws_instance" "test" {
  launch_template {
    name = aws_launch_template.test.name
  }
}
`, rName))
}
