package recipes

import "math"

// Unit systems.
const (
	Metric   = "metric"
	Imperial = "imperial"
)

// ConvertUnits converts a quantity into the requested unit system. Units
// that are the same in both systems (tsp, tbsp, pieces…) and unknown units
// are returned unchanged. Results are rounded to kitchen-friendly values.
func ConvertUnits(q float64, unit, system string) (float64, string) {
	switch system {
	case Metric:
		switch unit {
		case "oz":
			return metricMass(q * 28.3495)
		case "lb":
			return metricMass(q * 453.592)
		case "cup":
			return metricVolume(q * 236.6)
		case "fl oz":
			return metricVolume(q * 29.57)
		}
	case Imperial:
		switch unit {
		case "g":
			return imperialMass(q / 28.3495)
		case "kg":
			return imperialMass(q * 1000 / 28.3495)
		case "ml":
			return imperialVolume(q)
		case "l":
			return imperialVolume(q * 1000)
		}
	}
	return q, unit
}

func metricMass(g float64) (float64, string) {
	if g >= 1000 {
		return round(g/1000, 0.05), "kg"
	}
	return roundMetric(g), "g"
}

func metricVolume(ml float64) (float64, string) {
	if ml >= 1000 {
		return round(ml/1000, 0.05), "l"
	}
	return roundMetric(ml), "ml"
}

func roundMetric(v float64) float64 {
	switch {
	case v < 10:
		return round(v, 0.5)
	case v < 100:
		return round(v, 5)
	default:
		return round(v, 10)
	}
}

func imperialMass(oz float64) (float64, string) {
	if oz >= 16 {
		return round(oz/16, 0.25), "lb"
	}
	return round(oz, 0.25), "oz"
}

func imperialVolume(ml float64) (float64, string) {
	switch {
	case ml < 14:
		return round(ml/4.93, 0.25), "tsp"
	case ml < 59:
		return round(ml/14.79, 0.25), "tbsp"
	default:
		return round(ml/236.6, 0.25), "cup"
	}
}

func round(v, step float64) float64 {
	r := math.Round(v/step) * step
	if r == 0 && v > 0 {
		return step
	}
	return math.Round(r*100) / 100
}
