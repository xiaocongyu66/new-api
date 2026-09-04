package egress

// optionRow mirrors the options table row this package reads when resolving the
// stored proxy configuration. It is declared locally so egress does not import
// the model layer, which would close a cycle
// (catalog -> egress -> model -> catalog).
type optionRow struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

func (optionRow) TableName() string { return "options" }
