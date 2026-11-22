package merchant

import "strings"

// Merchant partitions. The merchant profile's service_types field now carries
// exactly one of these values; the old delivery/pickup vocabulary is no longer
// a merchant attribute.
const (
	CategoryFood     = "FOOD"
	CategorySupplies = "SUPPLIES"
	CategoryOther    = "OTHER"
)

var categoryOrder = []string{CategoryFood, CategorySupplies, CategoryOther}

// Categories returns the partition values in display order. Callers must copy
// the slice before mutating it.
func Categories() []string {
	out := make([]string, len(categoryOrder))
	copy(out, categoryOrder)
	return out
}

func ValidCategory(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case CategoryFood, CategorySupplies, CategoryOther:
		return true
	default:
		return false
	}
}

// NormalizeServiceTypes keeps only the three merchant partitions. It is used by
// the write path; historical rows are tolerated at read time through Category.
func NormalizeServiceTypes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if !ValidCategory(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// Category returns the merchant's single ranking partition. Historical rows
// that still carry DELIVERY/PICKUP are treated as OTHER so the hot list never
// drops a merchant because of legacy data.
func (p Profile) Category() string {
	if len(p.ServiceTypes) == 0 {
		return CategoryOther
	}
	value := strings.ToUpper(strings.TrimSpace(p.ServiceTypes[0]))
	if ValidCategory(value) {
		return value
	}
	return CategoryOther
}
