package client

import (
	"testing"
	"time"

	"github.com/cleardataeng/step/aws/mocks"
	"github.com/cleardataeng/step/bifrost"
	"github.com/cleardataeng/step/deployer"
	"github.com/cleardataeng/step/machine"
	"github.com/cleardataeng/step/utils/to"
	"github.com/stretchr/testify/assert"
)

func Test_Client_PrepareReleaseBundle(t *testing.T) {
	awsc := mocks.MockAwsClients()
	release := &deployer.Release{
		Release: bifrost.Release{
			AwsRegion:    to.Strp("project"),
			AwsAccountID: to.Strp("project"),
			ReleaseID:    to.TimeUUID("release-"),
			CreatedAt:    to.Timep(time.Now()),
			ProjectName:  to.Strp("project"),
			ConfigName:   to.Strp("project"),
			Bucket:       to.Strp("project"),
		},
		LambdaName:       to.Strp("project"),
		StepFnName:       to.Strp("project"),
		StateMachineJSON: to.Strp(machine.EmptyStateMachine),
	}

	err := PrepareReleaseBundle(
		awsc,
		release,
		to.Strp("../resources/empty_lambda.zip"), // Location to empty zip file
	)

	assert.NoError(t, err)
}
