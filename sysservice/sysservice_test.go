package sysservice

import (
	"strings"
	"testing"
)

const validUnit = `[Unit]
Description=Example tool

[Service]
User=tool
Group=tool
ExecStart=/usr/local/bin/tool serve --flag
NoNewPrivileges=yes
ProtectSystem=strict
PrivateTmp=yes
ConfigurationDirectory=tool
StateDirectory=tool
SupplementaryGroups=audio video
ReadWritePaths=/etc/tool

[Install]
WantedBy=multi-user.target
`

var validRules = UnitRules{Program: "tool", Groups: []string{"video", "audio"}, WritableConfig: true}

func TestUnitEntryAndInstallPaths(t *testing.T) {
	if got := UnitEntry("tool"); got != "lib/systemd/system/tool.service" {
		t.Fatalf("UnitEntry = %q", got)
	}
	want := Paths{
		Program: "/usr/local/bin/tool",
		Unit:    "/usr/local/lib/systemd/system/tool.service",
		Config:  "/etc/tool",
		State:   "/var/lib/tool",
	}
	if got := InstallPaths("tool"); got != want {
		t.Fatalf("InstallPaths = %+v, want %+v", got, want)
	}
}

func TestValidateUnitAccepts(t *testing.T) {
	noGroups := strings.Replace(validUnit, "SupplementaryGroups=audio video\n", "", 1)
	tests := map[string]struct {
		unit  string
		rules UnitRules
	}{
		"groups and writable config": {validUnit, validRules},
		"no groups, directive absent": {
			strings.Replace(noGroups, "ReadWritePaths=/etc/tool\n", "", 1),
			UnitRules{Program: "tool"},
		},
		"no groups, directive empty": {
			strings.Replace(noGroups, "NoNewPrivileges=yes", "SupplementaryGroups=\nNoNewPrivileges=yes", 1),
			UnitRules{Program: "tool", WritableConfig: true},
		},
		"equivalent values and spacing": {
			strings.NewReplacer("NoNewPrivileges=yes", "NoNewPrivileges = TRUE", "ProtectSystem=strict", "ProtectSystem=full", "PrivateTmp=yes", "PrivateTmp=1").Replace(validUnit),
			validRules,
		},
		"comments and CRLF": {
			"# c\r\n; c\r\n" + strings.ReplaceAll(validUnit, "\n", "\r\n"),
			validRules,
		},
		"groups across lines and continuation": {
			strings.Replace(validUnit, "SupplementaryGroups=audio video", "SupplementaryGroups=audio\nSupplementaryGroups=\\\n# c\nvideo \\\n audio", 1),
			validRules,
		},
		"later override stays valid": {
			validUnit + "\n[Service]\nPrivateTmp=no\nPrivateTmp=yes\nDynamicUser=no\n",
			validRules,
		},
		"exec without arguments and with harmless prefixes elsewhere": {
			strings.Replace(validUnit, "ExecStart=/usr/local/bin/tool serve --flag", "ExecStart=/usr/local/bin/tool\nExecStartPre=-/bin/true", 1),
			validRules,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if err := ValidateUnit([]byte(tc.unit), tc.rules); err != nil {
				t.Fatalf("ValidateUnit: %v", err)
			}
		})
	}
}

func TestValidateUnitRejects(t *testing.T) {
	rep := func(old, new string) string {
		if !strings.Contains(validUnit, old) {
			t.Fatalf("fixture lacks %q", old)
		}
		return strings.Replace(validUnit, old, new, 1)
	}
	// add appends line as the last assignment of [Service], so it can override everything before it.
	add := func(line string) string {
		return rep("ReadWritePaths=/etc/tool\n", "ReadWritePaths=/etc/tool\n"+line+"\n")
	}
	tests := map[string]struct {
		unit  string
		rules UnitRules
		want  string
	}{
		"missing Service section":      {"[Unit]\nDescription=x\n", validRules, "no [Service] section"},
		"section name case":            {strings.Replace(validUnit, "[Service]", "[service]", 1), validRules, "no [Service] section"},
		"assignment outside section":   {"User=tool\n" + validUnit, validRules, "outside a section"},
		"line without assignment":      {add("garbage"), validRules, "not a key=value"},
		"malformed section":            {add("[Service"), validRules, "malformed section"},
		"missing User":                 {rep("User=tool\n", ""), validRules, "User must be"},
		"other User":                   {rep("User=tool", "User=other"), validRules, "User must be"},
		"User reset by later line":     {add("User="), validRules, "User must be"},
		"User overridden later":        {add("User=other"), validRules, "User must be"},
		"User root":                    {rep("User=tool", "User=root"), validRules, "User must not be root"},
		"User root then overridden":    {add("User=root\nUser=tool"), validRules, "User must not be root"},
		"User numeric zero":            {rep("User=tool", "User=0"), validRules, "User must not be root"},
		"User root in Unit section":    {rep("Description=Example tool", "Description=x\nUser=root"), validRules, "User must not be root"},
		"missing Group":                {rep("Group=tool\n", ""), validRules, "Group must be"},
		"other Group":                  {rep("Group=tool", "Group=other"), validRules, "Group must be"},
		"Group root":                   {rep("Group=tool", "Group=root"), validRules, "Group must not be root"},
		"Group numeric zero":           {rep("Group=tool", "Group=00"), validRules, "Group must not be root"},
		"NoNewPrivileges missing":      {rep("NoNewPrivileges=yes\n", ""), validRules, "NoNewPrivileges must be yes"},
		"NoNewPrivileges no":           {rep("NoNewPrivileges=yes", "NoNewPrivileges=no"), validRules, "NoNewPrivileges must be yes"},
		"NoNewPrivileges overridden":   {add("NoNewPrivileges=false"), validRules, "NoNewPrivileges must be yes"},
		"ProtectSystem missing":        {rep("ProtectSystem=strict\n", ""), validRules, "ProtectSystem must be"},
		"ProtectSystem yes":            {rep("ProtectSystem=strict", "ProtectSystem=yes"), validRules, "ProtectSystem must be"},
		"ProtectSystem overridden":     {add("ProtectSystem=no"), validRules, "ProtectSystem must be"},
		"PrivateTmp missing":           {rep("PrivateTmp=yes\n", ""), validRules, "PrivateTmp must be yes"},
		"PrivateTmp no":                {rep("PrivateTmp=yes", "PrivateTmp=no"), validRules, "PrivateTmp must be yes"},
		"ConfigurationDirectory gone":  {rep("ConfigurationDirectory=tool\n", ""), validRules, "ConfigurationDirectory must be"},
		"ConfigurationDirectory other": {rep("ConfigurationDirectory=tool", "ConfigurationDirectory=other"), validRules, "ConfigurationDirectory must be"},
		"ConfigurationDirectory extra": {rep("ConfigurationDirectory=tool", "ConfigurationDirectory=tool other"), validRules, "ConfigurationDirectory must be"},
		"StateDirectory gone":          {rep("StateDirectory=tool\n", ""), validRules, "StateDirectory must be"},
		"StateDirectory other":         {rep("StateDirectory=tool", "StateDirectory=other"), validRules, "StateDirectory must be"},
		"StateDirectory extra":         {add("StateDirectory=other"), validRules, "StateDirectory must be"},
		"ExecStart missing":            {rep("ExecStart=/usr/local/bin/tool serve --flag\n", ""), validRules, "ExecStart is missing"},
		"ExecStart other binary":       {rep("/usr/local/bin/tool serve", "/usr/bin/tool serve"), validRules, "ExecStart must begin"},
		"ExecStart longer word":        {rep("/usr/local/bin/tool serve", "/usr/local/bin/toolx serve"), validRules, "ExecStart must begin"},
		"ExecStart second entry":       {add("ExecStart=/bin/sh -c x"), validRules, "ExecStart must begin"},
		"ExecStart ignore-fail prefix": {rep("ExecStart=/usr", "ExecStart=-/usr"), validRules, "ExecStart must begin"},
		"ExecStart continuation":       {rep("ExecStart=/usr/local/bin/tool serve", "ExecStart=\\\n/bin/sh"), validRules, "ExecStart must begin"},
		"groups missing":               {rep("SupplementaryGroups=audio video\n", ""), validRules, "SupplementaryGroups must be"},
		"groups extra":                 {rep("audio video", "audio video disk"), validRules, "SupplementaryGroups must be"},
		"groups fewer":                 {rep("audio video", "audio"), validRules, "SupplementaryGroups must be"},
		"groups reset":                 {add("SupplementaryGroups="), validRules, "SupplementaryGroups must be"},
		"groups unexpected":            {validUnit, UnitRules{Program: "tool", WritableConfig: true}, "SupplementaryGroups must be"},
		"writable config missing":      {rep("ReadWritePaths=/etc/tool\n", ""), validRules, "ReadWritePaths must release /etc/tool"},
		"writable config other path":   {rep("ReadWritePaths=/etc/tool", "ReadWritePaths=/etc/toolx"), validRules, "ReadWritePaths must release /etc/tool"},
		"writable config reset":        {add("ReadWritePaths="), validRules, "ReadWritePaths must release /etc/tool"},
		"DynamicUser yes":              {add("DynamicUser=yes"), validRules, "DynamicUser is not allowed"},
		"DynamicUser then false":       {add("DynamicUser=true\nDynamicUser=no"), validRules, "DynamicUser is not allowed"},
		"DynamicUser unparsable":       {add("DynamicUser=maybe"), validRules, "DynamicUser is not allowed"},
		"DynamicUser in Unit section":  {rep("Description=Example tool", "DynamicUser=1"), validRules, "DynamicUser is not allowed"},
		"AmbientCapabilities":          {add("AmbientCapabilities=CAP_NET_BIND_SERVICE"), validRules, "AmbientCapabilities is not allowed"},
		"AmbientCapabilities negated":  {add("AmbientCapabilities=~CAP_SYS_ADMIN"), validRules, "AmbientCapabilities is not allowed"},
		"PermissionsStartOnly":         {add("PermissionsStartOnly=yes"), validRules, "PermissionsStartOnly is not allowed"},
		"plus prefix":                  {add("ExecStartPre=+/bin/true"), validRules, "must not use the + or ! prefix"},
		"bang prefix":                  {add("ExecStartPost=!/bin/true"), validRules, "must not use the + or ! prefix"},
		"double bang prefix":           {add("ExecStop=!!/bin/true"), validRules, "must not use the + or ! prefix"},
		"combined prefix minus plus":   {add("ExecReload=-+/bin/true"), validRules, "must not use the + or ! prefix"},
		"combined prefix at bang":      {add("ExecStopPost=@!/bin/true tool"), validRules, "must not use the + or ! prefix"},
		"prefix on ExecCondition":      {add("ExecCondition=+/bin/true"), validRules, "must not use the + or ! prefix"},
		"prefix on ExecStart":          {rep("ExecStart=/usr", "ExecStart=+/usr"), validRules, "must not use the + or ! prefix"},
		"prefix via continuation":      {add("ExecStartPre=\\\n+/bin/true"), validRules, "must not use the + or ! prefix"},
		"prefix after spaced equals":   {add("ExecStartPre = +/bin/true"), validRules, "must not use the + or ! prefix"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateUnit([]byte(tc.unit), tc.rules)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateUnit error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestValidateUnitChecksProgramAndGroupsFirst(t *testing.T) {
	for name, rules := range map[string]UnitRules{
		"empty program":   {},
		"upper program":   {Program: "Tool"},
		"long program":    {Program: strings.Repeat("a", 33)},
		"program slash":   {Program: "a/b"},
		"program dot":     {Program: "a.b"},
		"root group":      {Program: "tool", Groups: []string{"root"}},
		"duplicate group": {Program: "tool", Groups: []string{"audio", "audio"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateUnit([]byte(validUnit), rules); err == nil {
				t.Fatal("ValidateUnit accepted invalid rules")
			}
		})
	}
}

func TestValidateGroups(t *testing.T) {
	for _, ok := range [][]string{nil, {"audio"}, {"_x", "a-b_c", strings.Repeat("a", 32)}} {
		if err := ValidateGroups("tool", ok); err != nil {
			t.Errorf("ValidateGroups(%q) = %v", ok, err)
		}
	}
	tests := map[string][]string{
		"empty name":      {""},
		"upper case":      {"Audio"},
		"digit first":     {"1abc"},
		"dash first":      {"-abc"},
		"too long":        {strings.Repeat("a", 33)},
		"dot":             {"a.b"},
		"space":           {"a b"},
		"duplicate":       {"audio", "video", "audio"},
		"program itself":  {"tool"},
		"root":            {"root"},
		"sudo":            {"sudo"},
		"wheel":           {"wheel"},
		"admin":           {"admin"},
		"one of many bad": {"audio", "wheel"},
	}
	for name, groups := range tests {
		t.Run(name, func(t *testing.T) {
			if err := ValidateGroups("tool", groups); err == nil {
				t.Fatalf("ValidateGroups(%q) accepted", groups)
			}
		})
	}
	if err := ValidateGroups("Bad", nil); err == nil {
		t.Fatal("ValidateGroups accepted an invalid program name")
	}
}
