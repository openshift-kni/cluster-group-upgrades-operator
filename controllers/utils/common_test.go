package utils

import (
	"testing"

	ranv1alpha1 "github.com/openshift-kni/cluster-group-upgrades-operator/pkg/api/clustergroupupgrades/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewSafeResourceNames(t *testing.T) {

	name := "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy"
	namespace := "ztp-install"
	safeName := NewSafeResourceName(name, namespace, "", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot-len(namespace), len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-len(namespace)-6]+"-", safeName[:MaxPolicyNameLengthExcludingTheDot-len(namespace)-5])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-config"
	safeName = NewSafeResourceName(name, "", "", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-6]+"-", safeName[:MaxPolicyNameLengthExcludingTheDot-5])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-placement"
	safeName = NewSafeResourceName(name, "", "", MaxObjectNameLength)
	assert.Equal(t, len(name)+6, len(safeName))
	assert.Equal(t, name+"-", safeName[:len(name)+1])

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy"
	safeName = NewSafeResourceName(name, namespace, "kuttl", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot-len(namespace), len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-len(namespace)-6]+"-kuttl", safeName)

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-config"
	safeName = NewSafeResourceName(name, "", "kuttl", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-6]+"-kuttl", safeName)

	name = "cnfdf18-new-common-cnfdf18-looooong-subscriptions-policy-placement"
	safeName = NewSafeResourceName(name, "", "kuttl", MaxObjectNameLength)
	assert.Equal(t, len(name)+6, len(safeName))
	assert.Equal(t, name+"-kuttl", safeName)

	name = "cgu-sriov-cloudransno-site9-spree-lb-du-cvslcm-4.14.0-rc.4-config"
	safeName = NewSafeResourceName(name, "", "", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-6]+"-", safeName[:MaxPolicyNameLengthExcludingTheDot-5])

	name = "cgu-sriov-cloudransno-site9-spree-lb-du-cvslcm-4.14.0-rc.4"
	safeName = NewSafeResourceName(name, "", "12345678", MaxPolicyNameLengthExcludingTheDot)
	assert.Equal(t, MaxPolicyNameLengthExcludingTheDot, len(safeName))
	assert.Equal(t, name[:MaxPolicyNameLengthExcludingTheDot-9]+"-", safeName[:MaxPolicyNameLengthExcludingTheDot-8])
}

func TestNewSafeResourceNameTrimsInvalidTrailingCharacters(t *testing.T) {
	// maxLength 10 with a 5-char suffix leaves 4 chars, "abc.", and the trailing
	// '.' is dropped because it may not precede the "-" separator.
	assert.Equal(t, "abc-kuttl", NewSafeResourceName("abc.defghijk", "", "kuttl", 10))
	assert.Equal(t, "abc.de-kuttl", NewSafeResourceName("abc.defghijk", "", "kuttl", 12))
}

func TestGetSafeResourceName(t *testing.T) {
	cgu := &ranv1alpha1.ClusterGroupUpgrade{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{NameSuffixAnnotation: "kuttl"}},
	}

	first := GetSafeResourceName("policy", "ns", cgu, MaxObjectNameLength)
	assert.Equal(t, "policy-kuttl", first)
	assert.Equal(t, map[string]string{"ns/policy": "policy-kuttl"}, cgu.Status.SafeResourceNames)

	// A stored name is reused even if the suffix annotation changes.
	cgu.Annotations[NameSuffixAnnotation] = "other"
	assert.Equal(t, first, GetSafeResourceName("policy", "ns", cgu, MaxObjectNameLength))

	// The same name in another namespace gets its own entry.
	assert.Equal(t, "policy-other", GetSafeResourceName("policy", "ns2", cgu, MaxObjectNameLength))
	assert.Len(t, cgu.Status.SafeResourceNames, 2)
}

func TestGetSafeResourceNameRandomSuffixIsStable(t *testing.T) {
	cgu := &ranv1alpha1.ClusterGroupUpgrade{}
	first := GetSafeResourceName("policy", "ns", cgu, MaxObjectNameLength)
	assert.Len(t, first, len("policy-")+RandomNameSuffixLength)
	assert.Equal(t, first, GetSafeResourceName("policy", "ns", cgu, MaxObjectNameLength))
}

func TestGetMinOf3(t *testing.T) {
	tests := []struct {
		a, b, c, want int
	}{
		{1, 2, 3, 1},
		{2, 1, 3, 1},
		{3, 2, 1, 1},
		{2, 2, 2, 2},
		{-1, 0, -5, -5},
		{4, 4, 5, 4},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, GetMinOf3(tc.a, tc.b, tc.c), "GetMinOf3(%d, %d, %d)", tc.a, tc.b, tc.c)
	}
}

func TestFindStringInSliceAndContains(t *testing.T) {
	s := []string{"a", "b", "a"}
	tests := []struct {
		name      string
		slice     []string
		find      string
		wantIndex int
		wantFound bool
	}{
		{"first match wins", s, "a", 0, true},
		{"later element", s, "b", 1, true},
		{"missing", s, "c", -1, false},
		{"nil slice", nil, "a", -1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			index, found := FindStringInSlice(tc.slice, tc.find)
			assert.Equal(t, tc.wantIndex, index)
			assert.Equal(t, tc.wantFound, found)
			assert.Equal(t, tc.wantFound, Contains(tc.slice, tc.find))
		})
	}
}

func TestContainsTemplates(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{`{{hub fromConfigMap "" "cm" "key" hub}}`, true},
		{`value: '{{ fromSecret "ns" "s" "k" }}'`, true},
		{"plain value", false},
		{"{{ unterminated", false},
		{"", false},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, ContainsTemplates(tc.in), "ContainsTemplates(%q)", tc.in)
	}
}

func TestDifference(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want []string
	}{
		{"removes shared elements, keeps order", []string{"c1", "c2", "c3", "c4"}, []string{"c3", "c1"}, []string{"c2", "c4"}},
		{"nothing shared", []string{"c1"}, []string{"c2"}, []string{"c1"}},
		{"everything shared", []string{"c1", "c2"}, []string{"c2", "c1"}, nil},
		{"empty a", nil, []string{"c1"}, nil},
		{"empty b", []string{"c1"}, nil, []string{"c1"}},
		{"duplicates in a are kept", []string{"c1", "c1", "c2"}, []string{"c2"}, []string{"c1", "c1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Difference(tc.a, tc.b))
		})
	}
}

func TestSortCGUListByPlanIndex(t *testing.T) {
	names := func(list *ranv1alpha1.ClusterGroupUpgradeList) []string {
		var out []string
		for _, cgu := range list.Items {
			out = append(out, cgu.Name)
		}
		return out
	}
	list := func(names ...string) *ranv1alpha1.ClusterGroupUpgradeList {
		l := &ranv1alpha1.ClusterGroupUpgradeList{}
		for _, n := range names {
			l.Items = append(l.Items, ranv1alpha1.ClusterGroupUpgrade{ObjectMeta: metav1.ObjectMeta{Name: n}})
		}
		return l
	}

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "orders by plan index",
			in:   []string{"ibgu-upgrade-1", "ibgu-finalizeupgrade-2", "ibgu-prep-0"},
			want: []string{"ibgu-prep-0", "ibgu-upgrade-1", "ibgu-finalizeupgrade-2"},
		},
		{
			name: "two-digit index sorts after single-digit index",
			in:   []string{"ibgu-abort-10", "ibgu-prep-2", "ibgu-upgrade-11", "ibgu-prep-9"},
			want: []string{"ibgu-prep-2", "ibgu-prep-9", "ibgu-abort-10", "ibgu-upgrade-11"},
		},
		{
			name: "names without an index sort last, by name",
			in:   []string{"other-b", "ibgu-prep-1", "other-a"},
			want: []string{"ibgu-prep-1", "other-a", "other-b"},
		},
		{
			name: "equal indices keep their input order",
			in:   []string{"ibgu-upgrade-1", "other-ibgu-prep-1", "ibgu-prep-0"},
			want: []string{"ibgu-prep-0", "ibgu-upgrade-1", "other-ibgu-prep-1"},
		},
		{
			name: "mixed names in reverse order",
			in:   []string{"other-b", "ibgu-upgrade-12", "other-a", "ibgu-prep-3"},
			want: []string{"ibgu-prep-3", "ibgu-upgrade-12", "other-a", "other-b"},
		},
		{
			name: "empty list",
			in:   nil,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := list(tc.in...)
			SortCGUListByPlanIndex(l)
			assert.Equal(t, tc.want, names(l))
		})
	}
}
