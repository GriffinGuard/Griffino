package broker

import "errors"

var (
	ErrGeneratePassword    = errors.New("failed to generate rabbitmq password")
	ErrCreateUser          = errors.New("failed to create rabbitmq user")
	ErrSetPermissions      = errors.New("failed to set rabbitmq permissions")
	ErrDeclareQueue        = errors.New("failed to declare rabbitmq queue")
	ErrBindQueue           = errors.New("failed to bind rabbitmq queue")
	ErrDeleteUser          = errors.New("failed to delete rabbitmq user")
	ErrSetUserPassword     = errors.New("failed to set rabbitmq user password")
)