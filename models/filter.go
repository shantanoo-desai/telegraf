package models

import (
	"fmt"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
)

// TagFilter is the name of a tag, and the values on which to filter
type TagFilter struct {
	Name   string
	Values []string
	filter filter.Filter
}

func (tf *TagFilter) Compile() error {
	f, err := filter.Compile(tf.Values)
	if err != nil {
		return err
	}
	tf.filter = f
	return nil
}

// Filter containing drop/pass and tagdrop/tagpass rules
type Filter struct {
	NameDrop       []string
	nameDropFilter filter.Filter
	NamePass       []string
	namePassFilter filter.Filter

	FieldDrop       []string
	fieldDropFilter filter.Filter
	FieldPass       []string
	fieldPassFilter filter.Filter

	TagDropFilters []TagFilter
	TagPassFilters []TagFilter

	TagExclude       []string
	tagExcludeFilter filter.Filter
	TagInclude       []string
	tagIncludeFilter filter.Filter

	isActive bool
}

// Compile all Filter lists into filter.Filter objects.
func (f *Filter) Compile() error {
	if len(f.NameDrop) == 0 &&
		len(f.NamePass) == 0 &&
		len(f.FieldDrop) == 0 &&
		len(f.FieldPass) == 0 &&
		len(f.TagInclude) == 0 &&
		len(f.TagExclude) == 0 &&
		len(f.TagPassFilters) == 0 &&
		len(f.TagDropFilters) == 0 {
		return nil
	}

	f.isActive = true
	var err error
	f.nameDropFilter, err = filter.Compile(f.NameDrop)
	if err != nil {
		return fmt.Errorf("error compiling 'namedrop', %w", err)
	}
	f.namePassFilter, err = filter.Compile(f.NamePass)
	if err != nil {
		return fmt.Errorf("error compiling 'namepass', %w", err)
	}

	f.fieldDropFilter, err = filter.Compile(f.FieldDrop)
	if err != nil {
		return fmt.Errorf("error compiling 'fielddrop', %w", err)
	}
	f.fieldPassFilter, err = filter.Compile(f.FieldPass)
	if err != nil {
		return fmt.Errorf("error compiling 'fieldpass', %w", err)
	}

	f.tagExcludeFilter, err = filter.Compile(f.TagExclude)
	if err != nil {
		return fmt.Errorf("error compiling 'tagexclude', %w", err)
	}
	f.tagIncludeFilter, err = filter.Compile(f.TagInclude)
	if err != nil {
		return fmt.Errorf("error compiling 'taginclude', %w", err)
	}

	for i := 0; i < len(f.TagDropFilters); i++ {
		if err := f.TagDropFilters[i].Compile(); err != nil {
			return fmt.Errorf("error compiling 'tagdrop', %w", err)
		}
	}

	for i := 0; i < len(f.TagPassFilters); i++ {
		if err := f.TagPassFilters[i].Compile(); err != nil {
			return fmt.Errorf("error compiling 'tagpass', %w", err)
		}
	}
	return nil
}

// Select returns true if the metric matches according to the
// namepass/namedrop and tagpass/tagdrop filters.  The metric is not modified.
func (f *Filter) Select(metric telegraf.Metric) bool {
	if !f.isActive {
		return true
	}

	if !f.shouldNamePass(metric.Name()) {
		return false
	}

	if !f.shouldTagsPass(metric.TagList()) {
		return false
	}

	return true
}

// Modify removes any tags and fields from the metric according to the
// fieldpass/fielddrop and taginclude/tagexclude filters.
func (f *Filter) Modify(metric telegraf.Metric) {
	if !f.isActive {
		return
	}

	f.filterFields(metric)
	f.filterTags(metric)
}

// IsActive checking if filter is active
func (f *Filter) IsActive() bool {
	return f.isActive
}

// shouldNamePass returns true if the metric should pass, false if it should drop
// based on the drop/pass filter parameters
func (f *Filter) shouldNamePass(key string) bool {
	pass := func(f *Filter) bool {
		return f.namePassFilter.Match(key)
	}

	drop := func(f *Filter) bool {
		return !f.nameDropFilter.Match(key)
	}

	if f.namePassFilter != nil && f.nameDropFilter != nil {
		return pass(f) && drop(f)
	} else if f.namePassFilter != nil {
		return pass(f)
	} else if f.nameDropFilter != nil {
		return drop(f)
	}

	return true
}

// shouldFieldPass returns true if the metric should pass, false if it should drop
// based on the drop/pass filter parameters
func (f *Filter) shouldFieldPass(key string) bool {
	return ShouldPassFilters(f.fieldPassFilter, f.fieldDropFilter, key)
}

// shouldTagsPass returns true if the metric should pass, false if it should drop
// based on the tagdrop/tagpass filter parameters
func (f *Filter) shouldTagsPass(tags []*telegraf.Tag) bool {
	return ShouldTagsPass(f.TagPassFilters, f.TagDropFilters, tags)
}

// filterFields removes fields according to fieldpass/fielddrop.
func (f *Filter) filterFields(metric telegraf.Metric) {
	filterKeys := []string{}
	for _, field := range metric.FieldList() {
		if !f.shouldFieldPass(field.Key) {
			filterKeys = append(filterKeys, field.Key)
		}
	}

	for _, key := range filterKeys {
		metric.RemoveField(key)
	}
}

// filterTags removes tags according to taginclude/tagexclude.
func (f *Filter) filterTags(metric telegraf.Metric) {
	filterKeys := []string{}
	if f.tagIncludeFilter != nil {
		for _, tag := range metric.TagList() {
			if !f.tagIncludeFilter.Match(tag.Key) {
				filterKeys = append(filterKeys, tag.Key)
			}
		}
	}
	for _, key := range filterKeys {
		metric.RemoveTag(key)
	}

	if f.tagExcludeFilter != nil {
		for _, tag := range metric.TagList() {
			if f.tagExcludeFilter.Match(tag.Key) {
				filterKeys = append(filterKeys, tag.Key)
			}
		}
	}
	for _, key := range filterKeys {
		metric.RemoveTag(key)
	}
}

func ShouldPassFilters(include filter.Filter, exclude filter.Filter, key string) bool {
	if include != nil && exclude != nil {
		return include.Match(key) && !exclude.Match(key)
	} else if include != nil {
		return include.Match(key)
	} else if exclude != nil {
		return !exclude.Match(key)
	}
	return true
}

func ShouldTagsPass(passFilters []TagFilter, dropFilters []TagFilter, tags []*telegraf.Tag) bool {
	pass := func(tpf []TagFilter) bool {
		for _, pat := range tpf {
			if pat.filter == nil {
				continue
			}
			for _, tag := range tags {
				if tag.Key == pat.Name {
					if pat.filter.Match(tag.Value) {
						return true
					}
				}
			}
		}
		return false
	}

	drop := func(tdf []TagFilter) bool {
		for _, pat := range tdf {
			if pat.filter == nil {
				continue
			}
			for _, tag := range tags {
				if tag.Key == pat.Name {
					if pat.filter.Match(tag.Value) {
						return false
					}
				}
			}
		}
		return true
	}

	// Add additional logic in case where both parameters are set.
	// see: https://github.com/influxdata/telegraf/issues/2860
	if passFilters != nil && dropFilters != nil {
		// return true only in case when tag pass and won't be dropped (true, true).
		// in case when the same tag should be passed and dropped it will be dropped (true, false).
		return pass(passFilters) && drop(dropFilters)
	} else if passFilters != nil {
		return pass(passFilters)
	} else if dropFilters != nil {
		return drop(dropFilters)
	}

	return true
}
