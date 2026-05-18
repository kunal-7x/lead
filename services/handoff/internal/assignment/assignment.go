package assignment

import (
	"context"
	"errors"
	"sort"

	"github.com/lead/services/handoff/internal/model"
)

type OpenTaskCounter interface {
	CountOpenTasks(context.Context, string) (int, error)
}

var ErrNoEligibleSalesperson = errors.New("no eligible salesperson")

type Candidate struct {
	Rep       model.Salesperson
	OpenTasks int
}

func Select(ctx context.Context, counter OpenTaskCounter, reps []model.Salesperson, snapshot model.ScoringSnapshot) (model.Salesperson, error) {
	candidates := make([]Candidate, 0, len(reps))
	for _, rep := range reps {
		open, err := counter.CountOpenTasks(ctx, rep.ID)
		if err != nil {
			return model.Salesperson{}, err
		}
		if rep.MaxOpenTasks > 0 && open >= rep.MaxOpenTasks {
			continue
		}
		candidates = append(candidates, Candidate{Rep: rep, OpenTasks: open})
	}
	if len(candidates) == 0 {
		return model.Salesperson{}, ErrNoEligibleSalesperson
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		a := candidates[i]
		b := candidates[j]
		if a.OpenTasks != b.OpenTasks {
			return a.OpenTasks < b.OpenTasks
		}
		if isHot(snapshot.Temperature) && a.Rep.PerformanceScore != b.Rep.PerformanceScore {
			return a.Rep.PerformanceScore > b.Rep.PerformanceScore
		}
		if a.Rep.PerformanceScore != b.Rep.PerformanceScore {
			return weightedLoad(a) < weightedLoad(b)
		}
		return a.Rep.ID < b.Rep.ID
	})
	return candidates[0].Rep, nil
}

func isHot(temp model.Temperature) bool {
	return temp == model.TemperatureHot || temp == model.TemperatureSuperHot
}

func weightedLoad(candidate Candidate) float64 {
	weight := candidate.Rep.PerformanceScore
	if weight <= 0 {
		weight = 1
	}
	return float64(candidate.OpenTasks+1) / weight
}
