package model

import (
	"time"

	"github.com/google/uuid"
)

type OwnershipTransferStatus string

const (
	OwnershipTransferPending   OwnershipTransferStatus = "pending"
	OwnershipTransferAccepted  OwnershipTransferStatus = "accepted"
	OwnershipTransferRejected  OwnershipTransferStatus = "rejected"
	OwnershipTransferCancelled OwnershipTransferStatus = "cancelled"
)

type OwnershipTransfer struct {
	ID         uuid.UUID
	ServerID   uuid.UUID
	FromUserID string
	ToUserID   string
	Status     OwnershipTransferStatus
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

func (t OwnershipTransfer) IsPending() bool { return t.Status == OwnershipTransferPending }

type InitiateOwnershipTransferRequest struct {
	ToUserID string `json:"to_user_id"`
}

type OwnershipTransferResponse struct {
	ID         string     `json:"id"`
	ServerID   string     `json:"server_id"`
	FromUserID string     `json:"from_user_id"`
	ToUserID   string     `json:"to_user_id"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
}
