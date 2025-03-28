package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_truncateAnnotations(t *testing.T) {
	type args struct {
		anns          map[string]string
		maxSize       int
		truncatedAnns map[string]string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "maxSize = 0, don't truncate",
			args: args{
				anns:          map[string]string{"k": "v"},
				maxSize:       0,
				truncatedAnns: map[string]string{"k": "v"},
			},
		},
		{
			name: "maxSize = 1, don't truncate as there's no annotations that cna be truncated",
			args: args{
				anns:          map[string]string{"k": "v"},
				maxSize:       1,
				truncatedAnns: map[string]string{"k": "v"},
			},
		},
		{
			name: "truncate last element for batch clusters list",
			args: args{
				anns: map[string]string{
					"k":                                    "v",
					CGUEventAnnotationKeyBatchClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyBatchClustersList) + 10,
				truncatedAnns: map[string]string{
					"k":                                    "v",
					CGUEventAnnotationKeyBatchClustersList: "cluster1",
				},
			},
		},
		{
			name: "truncate last element for missing clusters list",
			args: args{
				anns: map[string]string{
					"k":                                      "v",
					CGUEventAnnotationKeyMissingClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyMissingClustersList) + 10,
				truncatedAnns: map[string]string{
					"k":                                      "v",
					CGUEventAnnotationKeyMissingClustersList: "cluster1",
				},
			},
		},
		{
			name: "truncate last element for missing clusters list",
			args: args{
				anns: map[string]string{
					"k": "v",
					CGUEventAnnotationKeyTimedoutClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyTimedoutClustersList) + 10,
				truncatedAnns: map[string]string{
					"k": "v",
					CGUEventAnnotationKeyTimedoutClustersList: "cluster1",
				},
			},
		},
		// Same as the previous 3 tcs, but don't truncate as there's room for all anns.
		{
			name: "truncate last element for batch clusters list",
			args: args{
				anns: map[string]string{
					"k":                                    "v",
					CGUEventAnnotationKeyBatchClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyBatchClustersList) + 100,
				truncatedAnns: map[string]string{
					"k":                                    "v",
					CGUEventAnnotationKeyBatchClustersList: "cluster1,cluster2",
				},
			},
		},
		{
			name: "truncate last element for missing clusters list",
			args: args{
				anns: map[string]string{
					"k":                                      "v",
					CGUEventAnnotationKeyMissingClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyMissingClustersList) + 100,
				truncatedAnns: map[string]string{
					"k":                                      "v",
					CGUEventAnnotationKeyMissingClustersList: "cluster1,cluster2",
				},
			},
		},
		{
			name: "truncate last element for missing clusters list",
			args: args{
				anns: map[string]string{
					"k": "v",
					CGUEventAnnotationKeyTimedoutClustersList: "cluster1,cluster2",
				},
				maxSize: len(CGUEventAnnotationKeyTimedoutClustersList) + 100,
				truncatedAnns: map[string]string{
					"k": "v",
					CGUEventAnnotationKeyTimedoutClustersList: "cluster1,cluster2",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncateAnnotations(tt.args.anns, tt.args.maxSize)
			assert.Equal(t, tt.args.anns, tt.args.truncatedAnns)
		})
	}
}

func Test_truncateListString(t *testing.T) {
	type args struct {
		listStr string
		maxSize int64
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "truncate all",
			args: args{
				listStr: "elem1, elem2",
				maxSize: 0,
			},
			want: "",
		},
		{
			name: "truncate second element",
			args: args{
				listStr: "elem1,elem2",
				maxSize: 5,
			},
			want: "elem1",
		},
		{
			name: "truncate last two elements",
			args: args{
				listStr: "elem1,elem2,elem3",
				maxSize: 5,
			},
			want: "elem1",
		},
		{
			name: "truncate last element",
			args: args{
				listStr: "elem1,elem2,elem3",
				maxSize: 11, // 5*2 + separator ","
			},
			want: "elem1,elem2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateListString(tt.args.listStr, tt.args.maxSize); got != tt.want {
				t.Errorf("truncateListString() = %v, want %v", got, tt.want)
			}
		})
	}
}
