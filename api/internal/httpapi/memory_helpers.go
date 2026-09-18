package httpapi

func zeroInt64ToNull(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
