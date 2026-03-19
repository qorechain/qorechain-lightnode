package delegation

// SplitResult describes how to split delegation across validators.
type SplitResult struct {
	Validator string
	Amount    int64 // uqor
	Weight    int
}

// ComputeSplit distributes an amount across validators by weight.
func ComputeSplit(validators []string, weights []int, totalAmount int64) []SplitResult {
	if len(validators) == 0 || totalAmount <= 0 {
		return nil
	}

	totalWeight := 0
	for _, w := range weights {
		totalWeight += w
	}
	if totalWeight == 0 {
		return nil
	}

	results := make([]SplitResult, len(validators))
	var allocated int64

	for i, v := range validators {
		w := weights[i]
		if i == len(validators)-1 {
			// Last validator gets remainder to avoid rounding loss
			results[i] = SplitResult{
				Validator: v,
				Amount:    totalAmount - allocated,
				Weight:    w,
			}
		} else {
			amount := totalAmount * int64(w) / int64(totalWeight)
			results[i] = SplitResult{
				Validator: v,
				Amount:    amount,
				Weight:    w,
			}
			allocated += amount
		}
	}
	return results
}
