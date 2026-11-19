package billing

// InviteRewardEvent records one eligible recharge for the inviter recharge
// rebate. Wallet top-ups, subscription purchases and allowlisted redemption
// codes each write a row here, so the "first" trigger can count qualifying
// recharges across all configured sources instead of only wallet TopUps.
// Rows are written in the same transaction as the recharge itself.
type InviteRewardEvent struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	UserId         int    `json:"user_id" gorm:"index"` // invitee
	Source         string `json:"source" gorm:"type:varchar(20)"`
	RedemptionName string `json:"redemption_name" gorm:"type:varchar(255)"`
	Quota          int    `json:"quota"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;index"`
}
