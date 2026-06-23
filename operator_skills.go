package main

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// operator_skills.go embeds the OPERATOR skills INTO the binary, so the agent driving mago always
// reads the guide that matches the exact binary it downloaded — current on every ship, offline, no
// website drift. (Distinct from skills.go, which is a company's runtime .mago/skills memory.)
// `mago skills` lists them; `mago skills <name>` prints one.

//go:embed skills/*.md
var operatorSkillsFS embed.FS

func cmdSkills(args []string) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name := strings.TrimSuffix(args[0], ".md")
		b, err := operatorSkillsFS.ReadFile("skills/" + name + ".md")
		if err != nil {
			return fmt.Errorf("no skill %q — run `mago skills` to list", name)
		}
		fmt.Print(string(b))
		if !strings.HasSuffix(string(b), "\n") {
			fmt.Println()
		}
		return nil
	}
	entries, err := fs.ReadDir(operatorSkillsFS, "skills")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	fmt.Printf("mago %s — operator skills (embedded in this binary):\n\n", version)
	for _, fn := range names {
		b, _ := operatorSkillsFS.ReadFile("skills/" + fn)
		fmt.Printf("  %-12s %s\n", strings.TrimSuffix(fn, ".md"), operatorSkillDescription(string(b)))
	}
	fmt.Printf("\nRead one:  mago skills <name>\n")
	return nil
}

// operatorSkillDescription returns a skill's `description:` frontmatter (or its first real line).
func operatorSkillDescription(md string) string {
	var first string
	for _, line := range strings.Split(md, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "description:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "description:"))
		}
		if first == "" && l != "" && l != "---" && !strings.HasPrefix(l, "name:") {
			first = strings.TrimLeft(l, "# ")
		}
	}
	return first
}
