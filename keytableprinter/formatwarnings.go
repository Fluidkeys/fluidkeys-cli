package keytableprinter

import (
	"fmt"

	"github.com/fluidkeys/fluidkeys/colour"
	"github.com/fluidkeys/fluidkeys/status"
)

// FormatKeyWarningLines takes a status.KeyWarning and returns an array of
// human friendly messages coloured appropriately for printing to the
// terminal.
func FormatKeyWarningLines(warning status.KeyWarning) []string {
	switch warning.(type) {

	case status.DueForRotation:
		return []string{colour.Warn("Due for rotation 🔄")}

	case status.OverdueForRotation:
		warnings := []string{
			colour.Red("Overdue for rotation ⏰"),
		}
		var additionalMessage string
		switch days := warning.(status.OverdueForRotation).DaysUntilExpiry; days {
		case 0:
			additionalMessage = "Expires today!"
		case 1:
			additionalMessage = "Expires tomorrow!"
		default:
			additionalMessage = fmt.Sprintf("Expires in %d days!", days)
		}
		return append(warnings, colour.Red(additionalMessage))

	case status.NoExpiry:
		return []string{colour.Red("No expiry date set 📅")}

	case status.LongExpiry:
		return []string{colour.Warn("Expiry date too far off 📅")}

	case status.Expired:
		var message string
		switch days := warning.(status.Expired).DaysSinceExpiry; days {
		case 0:
			message = "Expired today ⚰️"
		case 1:
			message = "Expired yesterday ⚰️"
		case 2, 3, 4, 5, 6, 7, 8, 9:
			message = fmt.Sprintf("Expired %d days ago ⚰️", days)
		default:
			message = "Expired"
		}
		return []string{colour.Grey(message)}

	default:
		// TODO: log this but silently swallow the error
		return []string{}
	}
}
