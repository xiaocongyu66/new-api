package billing

import (
	"testing"
)

// TestIsCheckinSwitchCommand 群级签到开关指令识别
func TestIsCheckinSwitchCommand(t *testing.T) {
	cases := []struct {
		content string
		want    string
		ok      bool
	}{
		{"/关闭签到", CmdDisableCheckin, true},
		{"/开启签到", CmdEnableCheckin, true},
		{"/签到状态", CmdCheckinStatus, true},
		{"<qqbot-at-user id=\"X\" /> /关闭签到", CmdDisableCheckin, true},
		{"  /关闭签到  ", CmdDisableCheckin, true},
		// 普通签到指令不能被误判成开关指令，否则签到功能直接坏掉
		{"/签到", "", false},
		{"签到", "", false},
		{"/开启掉落", "", false},
		{"关闭签到", "", false},
		{"我要关闭签到", "", false},
	}
	for _, c := range cases {
		got, ok := isCheckinSwitchCommand(c.content)
		if ok != c.ok || got != c.want {
			t.Fatalf("isCheckinSwitchCommand(%q) = (%q,%v), want (%q,%v)",
				c.content, got, ok, c.want, c.ok)
		}
	}
}

// TestCheckinAndSwitchCommandsDontOverlap 两套指令互不吞噬
func TestCheckinAndSwitchCommandsDontOverlap(t *testing.T) {
	// 开关指令不应被签到指令匹配
	for _, cmd := range []string{CmdDisableCheckin, CmdEnableCheckin, CmdCheckinStatus} {
		if isCheckinCommand(cmd) {
			t.Fatalf("%q 被误判为签到指令", cmd)
		}
	}
	// 签到指令不应被开关指令匹配
	for _, cmd := range []string{"/签到", "签到"} {
		if _, ok := isCheckinSwitchCommand(cmd); ok {
			t.Fatalf("%q 被误判为开关指令", cmd)
		}
	}
	// 开关指令也不该被掉落指令匹配
	for _, cmd := range []string{CmdDisableCheckin, CmdEnableCheckin, CmdCheckinStatus} {
		if _, ok := isDropCommand(cmd); ok {
			t.Fatalf("%q 被误判为掉落指令", cmd)
		}
	}
}

// TestParseGroupIDs 名单解析兼容多种分隔符
func TestParseGroupIDs(t *testing.T) {
	got := parseGroupIDs("AAA,BBB，CCC\nDDD EEE\t")
	want := []string{"AAA", "BBB", "CCC", "DDD", "EEE"}
	if len(got) != len(want) {
		t.Fatalf("解析结果数量不符: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("解析结果不符: %v", got)
		}
	}
	if got := parseGroupIDs("   "); got != nil {
		t.Fatalf("空串应返回 nil，实际 %v", got)
	}
	// 连续分隔符不该产生空项
	if got := parseGroupIDs("A,,,B"); len(got) != 2 {
		t.Fatalf("连续分隔符处理错误: %v", got)
	}
}

// TestIsCheckinDisabledGroup 黑名单判定
func TestIsCheckinDisabledGroup(t *testing.T) {
	s := GetQQBotSetting()
	original := s.CheckinDisabledGroups
	defer func() { s.CheckinDisabledGroups = original }()

	// 默认（空名单）所有群都能签到，不能因为新增开关而破坏现状
	s.CheckinDisabledGroups = ""
	if IsCheckinDisabledGroup("GROUP_A") {
		t.Fatal("空名单时不应禁用任何群")
	}

	s.CheckinDisabledGroups = "GROUP_A,GROUP_B"
	if !IsCheckinDisabledGroup("GROUP_A") {
		t.Fatal("GROUP_A 应被禁用")
	}
	if !IsCheckinDisabledGroup("GROUP_B") {
		t.Fatal("GROUP_B 应被禁用")
	}
	// 只关一个群，其他群必须照常
	if IsCheckinDisabledGroup("GROUP_C") {
		t.Fatal("GROUP_C 未在名单内，不应被禁用")
	}
	// 空 groupOpenID（网页签到等非群场景）不能被误伤
	if IsCheckinDisabledGroup("") {
		t.Fatal("空 groupOpenID 不应被判定为禁用")
	}
}

// TestContainsGroup 空值与常规匹配
func TestContainsGroup(t *testing.T) {
	list := []string{"X", "Y"}
	if !containsGroup(list, "X") {
		t.Fatal("X 应命中")
	}
	if containsGroup(list, "Z") {
		t.Fatal("Z 不该命中")
	}
	if containsGroup(list, "") {
		t.Fatal("空值不该命中")
	}
	if containsGroup(nil, "X") {
		t.Fatal("空列表不该命中")
	}
}

// TestDropAndCheckinListsAreIndependent 掉落名单与签到名单互不影响
func TestDropAndCheckinListsAreIndependent(t *testing.T) {
	s := GetQQBotSetting()
	origDrop, origCheckin := s.DropGroups, s.CheckinDisabledGroups
	defer func() {
		s.DropGroups = origDrop
		s.CheckinDisabledGroups = origCheckin
	}()

	// 同一个群：开着掉落、关掉签到，两者必须能并存
	s.DropGroups = "GROUP_A"
	s.CheckinDisabledGroups = "GROUP_A"
	if !IsDropGroup("GROUP_A") {
		t.Fatal("GROUP_A 掉落应仍然开启")
	}
	if !IsCheckinDisabledGroup("GROUP_A") {
		t.Fatal("GROUP_A 签到应已关闭")
	}
}
