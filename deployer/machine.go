package deployer

import (
	"github.com/cleardataeng/step/aws"
	"github.com/cleardataeng/step/handler"
	"github.com/cleardataeng/step/machine"
)

// StateMachine returns the StateMachine for the deployer
func StateMachine() (*machine.StateMachine, error) {
	return machine.FromJSON([]byte(`{
    "Comment": "Step Function Deployer",
    "StartAt": "Validate",
    "States": {
      "Validate": {
        "Type": "TaskFn",
        "Resource": "arn:aws:lambda:{{aws_region}}:{{aws_account}}:function:{{lambda_name}}",
        "Comment": "Validate and Set Defaults",
        "Next": "Lock",
        "Catch": [
          {
            "Comment": "Bad Release or Error GoTo end",
            "ErrorEquals": ["States.ALL"],
            "ResultPath": "$.error",
            "Next": "FailureClean"
          }
        ]
      },
      "Lock": {
        "Type": "TaskFn",
        "Resource": "arn:aws:lambda:{{aws_region}}:{{aws_account}}:function:{{lambda_name}}",
        "Comment": "Grab Lock",
        "Next": "ValidateResources",
        "Catch": [
          {
            "Comment": "Something else is deploying",
            "ErrorEquals": ["LockExistsError"],
            "ResultPath": "$.error",
            "Next": "FailureClean"
          },
          {
            "Comment": "Try Release Lock Then Fail",
            "ErrorEquals": ["States.ALL"],
            "ResultPath": "$.error",
            "Next": "ReleaseLockFailure"
          }
        ]
      },
      "ValidateResources": {
        "Type": "TaskFn",
        "Resource": "arn:aws:lambda:{{aws_region}}:{{aws_account}}:function:{{lambda_name}}",
        "Comment": "ValidateResources",
        "Next": "Deploy",
        "Catch": [
          {
            "Comment": "Try Release Lock Then Fail",
            "ErrorEquals": ["States.ALL"],
            "ResultPath": "$.error",
            "Next": "ReleaseLockFailure"
          }
        ]
      },
      "Deploy": {
        "Type": "TaskFn",
        "Resource": "arn:aws:lambda:{{aws_region}}:{{aws_account}}:function:{{lambda_name}}",
        "Comment": "Upload Step-Function and Lambda",
        "Next": "Success",
        "Catch": [
          {
            "Comment": "Unsure of State, Leave Lock and Fail",
            "ErrorEquals": ["DeploySFNError"],
            "ResultPath": "$.error",
            "Next": "ReleaseLockFailure"
          },
          {
            "Comment": "Unsure of State, Leave Lock and Fail",
            "ErrorEquals": ["States.ALL"],
            "ResultPath": "$.error",
            "Next": "FailureDirty"
          }
        ]
      },
      "ReleaseLockFailure": {
        "Type": "TaskFn",
        "Resource": "arn:aws:lambda:{{aws_region}}:{{aws_account}}:function:{{lambda_name}}",
        "Comment": "Release the Lock and Fail",
        "Next": "FailureClean",
        "Retry": [ {
          "Comment": "Keep trying to Release",
          "ErrorEquals": ["States.ALL"],
          "MaxAttempts": 3,
          "IntervalSeconds": 30
        }],
        "Catch": [{
          "ErrorEquals": ["States.ALL"],
          "ResultPath": "$.error",
          "Next": "FailureDirty"
        }]
      },
      "FailureClean": {
        "Comment": "Deploy Failed Cleanly",
        "Type": "Fail",
        "Error": "NotifyError"
      },
      "FailureDirty": {
        "Comment": "Deploy Failed, Resources left in Bad State, ALERT!",
        "Type": "Fail",
        "Error": "AlertError"
      },
      "Success": {
        "Type": "Succeed"
      }
    }
  }`))
}

// TaskHandlers returns
func TaskHandlers() *handler.TaskHandlers {
	return CreateTaskFunctions(&aws.Clients{})
}

// CreateTaskFunctions returns
func CreateTaskFunctions(awsc aws.AwsClients) *handler.TaskHandlers {
	tm := handler.TaskHandlers{}
	tm["Validate"] = ValidateHandler(awsc)
	tm["Lock"] = LockHandler(awsc)
	tm["ValidateResources"] = ValidateResourcesHandler(awsc)
	tm["Deploy"] = DeployHandler(awsc)
	tm["ReleaseLockFailure"] = ReleaseLockFailureHandler(awsc)
	return &tm
}
