package catalog

import (
	"context"
	"sort"
)

// Related returns items like the given movie or show, scored by shared
// directors/creators, writers, leading cast and genres.
func (s *Store) Related(ctx context.Context, itemType, id string, limit int) ([]Card, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c2.item_id, SUM(CASE c.role WHEN 'director' THEN 6 WHEN 'creator' THEN 6 WHEN 'writer' THEN 3
			ELSE CASE WHEN c.ord < 5 AND c2.ord < 10 THEN 3 ELSE 1 END END)
		FROM credits c
		JOIN credits c2 ON c2.person_id = c.person_id AND c2.item_type = c.item_type AND c2.item_id != c.item_id
		WHERE c.item_type = ? AND c.item_id = ? AND (c.role != 'cast' OR c.ord < 15) AND (c2.role != 'cast' OR c2.ord < 20)
		GROUP BY c2.item_id`, itemType, id)
	if err != nil {
		return nil, err
	}
	scores := map[string]float64{}
	for rows.Next() {
		var other string
		var sc float64
		if err := rows.Scan(&other, &sc); err != nil {
			_ = rows.Close()
			return nil, err
		}
		scores[other] = sc
	}
	_ = rows.Close()

	var all []Card
	if itemType == "show" {
		all, err = s.Shows(ctx, Filter{})
	} else {
		all, err = s.Movies(ctx, Filter{})
	}
	if err != nil {
		return nil, err
	}
	var self *Card
	for i := range all {
		if all[i].ID == id {
			self = &all[i]
		}
	}
	if self == nil {
		return []Card{}, nil
	}
	genres := map[string]bool{}
	for _, g := range self.Genres {
		genres[g] = true
	}
	type scored struct {
		c Card
		s float64
	}
	var list []scored
	for _, c := range all {
		if c.ID == id {
			continue
		}
		sc := scores[c.ID]
		shared := 0
		for _, g := range c.Genres {
			if genres[g] {
				shared++
			}
		}
		if len(genres) > 0 {
			sc += 4 * float64(shared) / float64(len(genres))
		}
		if self.Year > 0 && c.Year > 0 {
			d := self.Year - c.Year
			if d < 0 {
				d = -d
			}
			if d <= 5 {
				sc += 0.5
			}
		}
		r := c.IMDbRating
		if r == 0 {
			r = c.Rating
		}
		sc += r / 20
		if sc >= 2 {
			list = append(list, scored{c, sc})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].s > list[j].s })
	out := []Card{}
	for i := 0; i < len(list) && i < limit; i++ {
		out = append(out, list[i].c)
	}
	return out, nil
}
