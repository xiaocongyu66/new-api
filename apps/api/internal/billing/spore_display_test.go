/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSporeDisplayFallbacks verifies the /api/status spore display contract:
// an empty name falls back to 「菌种」 and an empty symbol returns "" so the
// frontend hides the icon instead of rendering a placeholder glyph.
func TestSporeDisplayFallbacks(t *testing.T) {
	previous := generalSetting
	t.Cleanup(func() { generalSetting = previous })

	generalSetting.SporeName = ""
	generalSetting.SporeSymbol = "  "
	assert.Equal(t, "菌种", GetSporeName())
	assert.Equal(t, "", GetSporeSymbol())

	generalSetting.SporeName = "孢子"
	generalSetting.SporeSymbol = " 🍄 "
	assert.Equal(t, "孢子", GetSporeName())
	assert.Equal(t, "🍄", GetSporeSymbol())
}
