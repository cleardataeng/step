package bifrost

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/coinbase/step/aws"
	"github.com/coinbase/step/aws/s3"
	"github.com/coinbase/step/errors"
	"github.com/coinbase/step/utils/is"
	"github.com/coinbase/step/utils/to"
)

type Locker interface {
	GrabLock(namespace string, lockPath string, uuid string, reason string) (bool, error)
	ReleaseLock(namespace string, lockPath string, uuid string) error
}

// ReleaseError contains the error and cause for the state machine
type ReleaseError struct {
	Error *string
	Cause *string
}

// Release is the Data Structure passed between Client and Deployer
type Release struct {
	AwsAccountID *string `json:"aws_account_id,omitempty"`
	AwsRegion    *string `json:"aws_region,omitempty"`

	ReleaseSHA256 string `json:"-"` // Not Set By Client, Not Marshalled

	UUID      *string `json:"uuid,omitempty"`       // Generated By server
	ReleaseID *string `json:"release_id,omitempty"` // Generated Client

	ProjectName *string `json:"project_name,omitempty"`
	ConfigName  *string `json:"config_name,omitempty"`
	Bucket      *string `json:"bucket,omitempty"` // Bucket with Additional Data in it

	CreatedAt *time.Time `json:"created_at,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`

	Timeout *int `json:"timeout,omitempty"` // How long should we try and deploy in seconds

	// Additional Metadata attached but should not be functional
	Metadata map[string]string `json:"metadata,omitempty"`

	Error   *ReleaseError `json:"error,omitempty"`
	Success *bool         `json:"success,omitempty"`
}

///////
// Validations
///////

// Validate checks
// 1. the attributes are assigned
// 2. the time frame of created at is valid
// 3. the uploaded release sha is correct (requires S3 and &Release{})
func (r *Release) Validate(s3c aws.S3API, cRelease interface{}) error {

	if reflect.ValueOf(cRelease).Kind() != reflect.Ptr {
		// This is more a compile time check, to make sure the deployer if a pointer
		return fmt.Errorf("Release passed to Validate must be pointer e.g. &Release{}")
	}

	if is.EmptyStr(r.AwsAccountID) {
		return fmt.Errorf("AwsAccountID must be defined")
	}

	if is.EmptyStr(r.AwsRegion) {
		return fmt.Errorf("AwsRegion must be defined")
	}

	if is.EmptyStr(r.UUID) {
		return fmt.Errorf("UUID must be set by server")
	}

	if is.EmptyStr(r.ReleaseID) {
		return fmt.Errorf("ReleaseID must be defined")
	}

	if is.EmptyStr(r.ProjectName) {
		return fmt.Errorf("ProjectName must be defined")
	}

	if is.EmptyStr(r.ConfigName) {
		return fmt.Errorf("ConfigName must be defined")
	}

	if is.EmptyStr(r.Bucket) {
		return fmt.Errorf("Bucket must be defined")
	}

	if r.Timeout == nil {
		return fmt.Errorf("Timeout must be defined")
	}

	if r.CreatedAt == nil {
		return fmt.Errorf("CreatedAt must be defined")
	}

	if r.StartedAt == nil {
		return fmt.Errorf("StartedAt must be defined")
	}

	// Created at date must be after 10 days ago, and before 2 mins from now (wiggle room)
	// This allows roll backs but protects against redeploying something very old
	if !is.WithinTimeFrame(r.CreatedAt, 10*24*time.Hour, 2*time.Minute) {
		return fmt.Errorf("Created at older than 10 days (or in the future)")
	}

	if err := r.validateReleaseSHA(s3c, cRelease); err != nil {
		return err
	}

	return nil
}

func (r *Release) validateReleaseSHA(s3c aws.S3API, cRelease interface{}) error {

	err := s3.GetStruct(s3c, r.Bucket, r.ReleasePath(), cRelease)
	if err != nil {
		return fmt.Errorf("Error Unmarshalling uploaded Release struct with %v", err.Error())
	}

	expected := to.SHA256Struct(cRelease)

	if expected != r.ReleaseSHA256 {
		return fmt.Errorf("Release SHA incorrect expected %v, got %v", expected, r.ReleaseSHA256)
	}

	return nil
}

///////
// Defaults
///////

// This is called initially to wipe values that are controlled by this release
func (r *Release) WipeControlledValues() {
	r.UUID = nil
	r.StartedAt = nil
	r.Success = nil
}

// SetDefaults is passed the region and account where the lambda is executed
// AND the default-bucket-prefix to calculate the default bucket name.
func (r *Release) SetDefaults(region *string, account *string, bucket_prefix string) {
	if is.EmptyStr(r.UUID) {
		r.UUID = to.TimeUUID("release-")
	}

	if r.StartedAt == nil {
		now := time.Now()
		r.StartedAt = &now
	}

	if is.EmptyStr(r.AwsRegion) {
		r.AwsRegion = region
	}

	if is.EmptyStr(r.AwsAccountID) {
		r.AwsAccountID = account
	}

	if is.EmptyStr(r.Bucket) && account != nil {
		// default bucket is the default account_id not the release id (which could be in a different account)
		r.Bucket = to.Strp(fmt.Sprintf("%v%v", bucket_prefix, *account))
	}

	if r.Timeout == nil {
		r.Timeout = to.Intp(600) // Default to 10 minutes
	}
}

///////
// Paths
///////

func (r *Release) ProjectDir() *string {
	s := fmt.Sprintf("%v/%v", *r.AwsAccountID, *r.ProjectName)
	return &s
}

func (r *Release) RootDir() *string {
	s := fmt.Sprintf("%v/%v", *r.ProjectDir(), *r.ConfigName)
	return &s
}

func (r *Release) ReleaseDir() *string {
	s := fmt.Sprintf("%v/%v", *r.RootDir(), *r.ReleaseID)
	return &s
}

func (r *Release) ReleasePath() *string {
	s := fmt.Sprintf("%v/release", *r.ReleaseDir())
	return &s
}

func (release *Release) LogPath() *string {
	s := fmt.Sprintf("%v/log", *release.ReleaseDir())
	return &s
}

func (r *Release) SharedProjectDir() *string {
	s := fmt.Sprintf("%v/_shared", *r.ProjectDir())
	return &s
}

///////
// Errors
///////

func (r *Release) ErrorPrefix() string {
	if r.ReleaseID == nil {
		return fmt.Sprintf("Release Error:")
	}

	return fmt.Sprintf("Release(%v) Error:", *r.ReleaseID)
}

func (r *Release) TimedOut() error {
	now := time.Now()

	if r.StartedAt == nil {
		r.StartedAt = &now
	}

	timeout := r.StartedAt.Add(time.Second * time.Duration(*r.Timeout))
	if now.After(timeout) {
		return fmt.Errorf("Timeout: Halting Release")
	}

	return nil
}

///////
// Lock
///////

// UnlockRootLock deletes the Lock File for the release
func (r *Release) UnlockRoot(locker Locker, lockTableName string) error {
	return locker.ReleaseLock(lockTableName, *r.RootLockPath(), *r.UUID)
}

// GrabLock retrieves the Lock returns LockExistsError, or LockError
func (r *Release) GrabLocks(s3c aws.S3API, locker Locker, lockTableName string) error {
	if err := r.CheckUserLock(s3c, *r.UserLockPath()); err != nil {
		return err
	}

	if err := r.GrabReleaseLock(s3c); err != nil {
		return err
	}

	if err := r.GrabRootLock(locker, lockTableName); err != nil {
		return err
	}

	return nil
}

func (r *Release) GrabRootLock(locker Locker, lockTableName string) error {
	return r.grabGenericLock(locker, lockTableName, *r.RootLockPath())
}

func (r *Release) GrabReleaseLock(s3c aws.S3API) error {
	return r.grabS3Lock(s3c, *r.ReleaseLockPath())
}

func (r *Release) CheckUserLock(s3c aws.S3API, lockPath string) error {
	err := s3.CheckUserLock(s3c, r.Bucket, &lockPath)
	if err != nil {
		return &errors.LockExistsError{fmt.Sprintf("CheckUserLock error: %v", err.Error())}
	}
	return nil
}

func (r *Release) grabS3Lock(s3c aws.S3API, lockPath string) error {
	grabbed, err := s3.GrabLock(s3c, r.Bucket, &lockPath, *r.UUID)

	// Check grabbed first because there are errors that can be thrown before anything is created
	if !grabbed {
		if err != nil {
			return &errors.LockExistsError{err.Error()}
		}

		return &errors.LockExistsError{
			fmt.Sprintf(
				"S3 Lock Already Exists at %v:%v\nRun the following to clear it: "+
					"aws s3 rm s3://%[1]v/%[2]v",
				*r.Bucket, lockPath,
			),
		}
	}

	// Error if MAYBE grabbed the lock and we should try to unlock
	if err != nil {
		return &errors.LockError{err.Error()}
	}

	return nil
}

func (r *Release) grabGenericLock(locker Locker, lockTableName string, lockPath string) error {
	grabbed, err := locker.GrabLock(lockTableName, lockPath, *r.UUID, "")

	// Check grabbed first because there are errors that can be thrown before anything is created
	if !grabbed {
		if err != nil {
			return &errors.LockExistsError{err.Error()}
		}

		return &errors.LockExistsError{
			fmt.Sprintf(
				"Lock Already Exists at %v:%v\nRun the following to clear it: "+
					"aws dynamodb delete-item --table-name %[1]v --key='{\"key\": {\"S\": \"%[2]v\" }}'",
				lockTableName, lockPath,
			),
		}
	}

	// Error if MAYBE grabbed the lock and we should try to unlock
	if err != nil {
		return &errors.LockError{err.Error()}
	}

	return nil
}

func (r *Release) ReleaseLockPath() *string {
	s := fmt.Sprintf("%v/lock", *r.ReleaseDir())
	return &s
}

func (r *Release) RootLockPath() *string {
	s := fmt.Sprintf("%v/lock", *r.RootDir())
	return &s
}

func (r *Release) UserLockPath() *string {
	s := fmt.Sprintf("%v/user-lock", *r.RootDir())
	return &s
}

/////////
// Halt
/////////

func (r *Release) HaltPath() *string {
	s := fmt.Sprintf("%v/halt", *r.RootDir())
	return &s
}

// IsHalt will error with if halt flag found
func (r *Release) IsHalt(s3c aws.S3API) error {

	if err := r.TimedOut(); err != nil {
		return err
	}

	if message := r.haltFlag(s3c); message != nil {
		if *message == "" {
			message = to.Strp("Halt File Found")
		}
		return fmt.Errorf(*message)
	}

	return nil
}

// Halt writes Halt flag to S3 to attempt to stop the release
func (r *Release) Halt(s3c aws.S3API, message *string) error {
	return s3.PutStr(s3c, r.Bucket, r.HaltPath(), message)
}

// RemoveHalt deletes the halt flat from S3
func (r *Release) RemoveHalt(s3c aws.S3API) {
	if err := s3.Delete(s3c, r.Bucket, r.HaltPath()); err != nil {
		// ignore errors
		fmt.Printf("Warning(RemoveHalt) error ignored: %v\n", err.Error())
	}
}

// Returns the haltFlag
func (r *Release) haltFlag(s3c aws.S3API) *string {
	output, body, err := s3.GetObject(s3c, r.Bucket, r.HaltPath())

	// If no file or any error return false
	if err != nil {
		return nil
	}

	// check halt was written in last 5 mins, and before a 2 mins in the future
	if !is.WithinTimeFrame(output.LastModified, 5*time.Minute, 2*time.Minute) {
		return nil
	}

	if body == nil {
		return to.Strp("")
	}

	return to.Strp(string(*body))
}

// ExecutionPrefix returns
func (r *Release) ExecutionPrefix() string {
	pn := strings.Replace(*r.ProjectName, "/", "-", -1)
	return fmt.Sprintf("deploy-%v-%v-", pn, *r.ConfigName)
}

// ExecutionName returns
func (r *Release) ExecutionName() *string {
	return to.TimeUUID(r.ExecutionPrefix())
}

///////
// Log
///////

func (release *Release) WriteLog(s3c aws.S3API, log string) error {
	if err := s3.PutStr(s3c, release.Bucket, release.LogPath(), &log); err != nil {
		return err
	}

	return nil
}

func (release *Release) AppendLog(s3c aws.S3API, log string) error {
	logFile, err := s3.GetStr(s3c, release.Bucket, release.LogPath())

	if err != nil {
		switch err.(type) {
		case *s3.NotFoundError:
			// no log yet
			logFile = to.Strp("")
		default:
			return err // All other errors return
		}
	}

	// doubly sure
	if logFile == nil {
		logFile = to.Strp("")
	}

	allLog := fmt.Sprintf("%v\n%v", *logFile, log)

	if err := s3.PutStr(s3c, release.Bucket, release.LogPath(), &allLog); err != nil {
		return err
	}

	return nil
}
