/*
Copyright 2025 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package classifier

import (
	"fmt"
)

// ErrLimitsMismatch is returned when the amount of limits does not match the
// amount of classifiers. Each classifier operates over a limit, so the amount
// of classifiers must match the amount of limits.
var ErrLimitsMismatch = fmt.Errorf("amount of limits must match the amount of classifiers")

// Classifier is a function that classifies a resource usage based on a limit.
// The function should return true if the resource usage matches the classifier
// intent.
type Classifier[V any] func(V, V) (bool, error)

// Comparer is a function that compares two objects. This function should return
// -1 if the first object is less than the second, 0 if they are equal, and 1 if
// the first object is greater than the second. Of course this is a simplification
// and any value between -1 and 1 can be returned.
type Comparer[V any] func(V, V) int

// Values is a map of values indexed by a comparable key. An example of this
// can be a list of resources indexed by a node name.
type Values[K comparable, V any] map[K]V

// Limits is a map of list of limits indexed by a comparable key. Each limit
// inside the list requires a classifier to evaluate.
type Limits[K comparable, V any] map[K][]V

// Classify is a function that classifies based on classifier functions. This
// function receives Values, a list of n Limits (indexed by name), and a list
// of n Classifiers. The classifier at n position is called to evaluate the
// limit at n position. The first classifier to return true will receive the
// value, at this point the loop will break and the next value will be
// evaluated. This function returns a slice of maps, each position in the
// returned slice correspond to one of the classifiers (e.g. if n limits
// and classifiers are provided, the returned slice will have n maps).
func Classify[K comparable, V any](
	values Values[K, V], limits Limits[K, V], classifiers ...Classifier[V],
) ([]map[K]V, error) {
	count := len(classifiers)
	for _, limit := range limits {
		if len(limit) != count {
			return nil, ErrLimitsMismatch
		}
	}

	result := make([]map[K]V, len(classifiers))
	for i := range classifiers {
		result[i] = make(map[K]V)
	}

	for index, usage := range values {
		for i, limit := range limits[index] {
			if done, err := classifiers[i](usage, limit); err != nil {
				return nil, err
			} else if done {
				result[i][index] = usage
				break
			}
		}
	}

	return result, nil
}

// ForMap is a function that returns a classifier that compares all values in a
// map. The function receives a Comparer function that is used to compare all
// the map values. The returned Classifier will return true only if the
// provided Comparer function returns a value less than 0 for all the values.
func ForMap[K comparable, V any, M ~map[K]V](cmp Comparer[V]) Classifier[M] {
	return func(data, limit M) (bool, error) {
		for idx, value := range data {
			if _, ok := limit[idx]; !ok {
				return false, fmt.Errorf("limit for %v not found", idx)
			}
			if cmp(value, limit[idx]) >= 0 {
				return false, nil
			}
		}
		return true, nil
	}
}
