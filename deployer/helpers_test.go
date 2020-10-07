package deployer

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/sfn"
	"github.com/cleardataeng/step/aws"
	"github.com/cleardataeng/step/aws/mocks"
	"github.com/cleardataeng/step/aws/s3"
	"github.com/cleardataeng/step/bifrost"
	"github.com/cleardataeng/step/machine"
	"github.com/cleardataeng/step/utils/to"
	"github.com/stretchr/testify/assert"
)

////////
// RELEASE
////////

func MockRelease() *Release {
	return &Release{
		Release: bifrost.Release{
			AwsAccountID: to.Strp("00000000"),
			ReleaseID:    to.Strp("release-1"),
			ProjectName:  to.Strp("project"),
			ConfigName:   to.Strp("development"),
			CreatedAt:    to.Timep(time.Now()),
			Metadata:     map[string]string{"User": "User@user.com"},
		},
		LambdaName:       to.Strp("lambdaname"),
		StepFnName:       to.Strp("stepfnname"),
		StateMachineJSON: to.Strp(machine.EmptyStateMachine),
	}
}

func MockAwsClients(r *Release) *mocks.MockClients {
	awsc := mocks.MockAwsClients()

	awsc.Lambda.ListTagsResp = &lambda.ListTagsOutput{
		Tags: map[string]*string{"ProjectName": r.ProjectName, "ConfigName": r.ConfigName, "DeployWith": to.Strp("step-deployer")},
	}

	awsc.SFN.DescribeStateMachineResp = &sfn.DescribeStateMachineOutput{
		RoleArn: to.Strp(fmt.Sprintf("arn:aws:iam::000000000000:role/step/%v/%v/role-name", *r.ProjectName, *r.ConfigName)),
	}

	lambda_zip_file_contents := "lambda_zip"
	awsc.S3.AddGetObject(*r.LambdaZipPath(), lambda_zip_file_contents, nil)

	if r.LambdaSHA256 == nil {
		r.LambdaSHA256 = to.Strp(to.SHA256Str(&lambda_zip_file_contents))
	}

	raw, _ := json.Marshal(r)

	account_id := r.AwsAccountID
	if account_id == nil {
		account_id = to.Strp("000000000000")
	}

	awsc.S3.AddGetObject(fmt.Sprintf("%v/%v/%v/%v/release", *account_id, *r.ProjectName, *r.ConfigName, *r.ReleaseID), string(raw), nil)

	return awsc
}

////////
// State Machine
////////

func createTestStateMachine(t *testing.T, awsc *mocks.MockClients) *machine.StateMachine {
	stateMachine, err := StateMachine()
	assert.NoError(t, err)

	tfs := CreateTaskFunctions(awsc)

	err = stateMachine.SetTaskFnHandlers(tfs)
	assert.NoError(t, err)

	return stateMachine
}

func assertNoRootLock(t *testing.T, awsc aws.AwsClients, release *Release) {
	_, err := s3.Get(awsc.S3Client(release.AwsRegion, nil, nil), release.Bucket, release.RootLockPath())
	assert.Error(t, err) // Not found error
	assert.IsType(t, &s3.NotFoundError{}, err)
}

func assertNoRootLockWithReleseLock(t *testing.T, awsc aws.AwsClients, release *Release) {
	assertNoRootLock(t, awsc, release)

	_, err := s3.Get(awsc.S3Client(release.AwsRegion, nil, nil), release.Bucket, release.ReleaseLockPath())
	assert.NoError(t, err) // Not error
}

func assertNoRootLockNoReleseLock(t *testing.T, awsc aws.AwsClients, release *Release) {
	assertNoRootLock(t, awsc, release)

	_, err := s3.Get(awsc.S3Client(release.AwsRegion, nil, nil), release.Bucket, release.ReleaseLockPath())
	assert.Error(t, err) // Not found error
	assert.IsType(t, &s3.NotFoundError{}, err)
}
