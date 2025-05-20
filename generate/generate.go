package generate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// RunOne lance la génération d'un seul modèle
func RunOne(srcPath string) error {
	return filepath.Walk(srcPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		return processFile(path)
	})
}

// Traite le fichier source
func processFile(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Read error: %w", err)
	}

	// IMPORTANT :
	// Voir si le fichier ou son contenu ne sont pas exclut
	// de l'écriture pour éviter de les modifiers
	if !shouldProcessFile(path, src) {
		return fmt.Errorf("file exclude: %s", path)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("Parse error: %w", err)
	}
	for _, decl := range node.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			st, ok2 := ts.Type.(*ast.StructType)
			if !ok || !ok2 || gd.Doc == nil {
				continue
			}
			var tableName string
			for _, c := range gd.Doc.List {
				if strings.Contains(c.Text, "ormx:generateModel") {
					tableName = extractTableName(c.Text)
					break
				}
			}
			if tableName == "" {
				continue
			}
			structName := ts.Name.Name
			receiver := strings.ToLower(string(structName[0]))
			end := fset.Position(st.End()).Offset

			genCode := generateModelMethods(structName, receiver, tableName, st)
			newSrc := append(src[:end], []byte(genCode)...)
			newSrc = ensureOrmxImport(newSrc)

			err = os.WriteFile(path, newSrc, 0644)
			if err != nil {
				return fmt.Errorf("Write error: %w", err)
			} else {
				fmt.Println("Code généré inséré dans :", path)
			}
			break
		}
	}

	return nil
}

// Extrait la valeur d'une clé depuis un commentaire du type "// ormx:generateModel table=unit_test"
func extractTableName(comment string) string {
	comment = strings.TrimSpace(comment)
	parts := strings.Fields(comment)
	for _, part := range parts {
		if strings.HasPrefix(part, "table=") {
			return strings.TrimPrefix(part, "table=")
		}
	}
	return ""
}

// Récupère la valeur d'une balise struct (ex: `db:"id"`)
func reflectTagValue(tag, key string) string {
	tag = strings.Trim(tag, "`")
	for _, part := range strings.Split(tag, " ") {
		if strings.HasPrefix(part, key+":") {
			val := strings.TrimPrefix(part, key+":")
			val = strings.Trim(val, "\"")
			return val
		}
	}
	return ""
}

// Pour exclure certains fichiers en fonction de leur contenu et leur nom de fichiers.
func shouldProcessFile(path string, src []byte) bool {
	base := filepath.Base(path)
	if base == "prototype.go" || base == "model_object.go" {
		return false
	}

	content := string(src)
	if !strings.Contains(content, "ormx:generateModel") {
		return false
	}
	if strings.Contains(content, "type Prototype struct") {
		return false
	}
	if strings.Contains(content, "type ModelObjet struct") {
		return false
	}

	return true
}

// Pour ajouter "ormx" dans import
func ensureOrmxImport(src []byte) []byte {
	srcStr := string(src)
	// Regex pour détecter "ormx" dans n'importe quel import (simple ou bloc)
	re := regexp.MustCompile(`import\s*(\([\s\S]*?"ormx"[\s\S]*?\)|"ormx")`)
	if re.MatchString(srcStr) {
		return src
	}

	lines := strings.Split(srcStr, "\n")
	insertedImport := false
	for i, line := range lines {
		if strings.HasPrefix(line, "import (") {
			lines = append(lines[:i+1], append([]string{"\t\"ormx\""}, lines[i+1:]...)...)
			insertedImport = true
			break
		}
		if strings.HasPrefix(line, "import ") && strings.Contains(line, "\"") {
			pkg := strings.TrimSpace(strings.TrimPrefix(line, "import"))
			pkg = strings.Trim(pkg, "\"")
			lines[i] = "import (\n\t\"" + pkg + "\"\n\t\"ormx\"\n)"
			insertedImport = true
			break
		}
	}
	if !insertedImport {
		for i, line := range lines {
			if strings.HasPrefix(line, "package ") {
				lines = append(lines[:i+1], append([]string{"import (\n\t\"ormx\"\n)"}, lines[i+1:]...)...)
				break
			}
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// Générer le code à insérer
func generateModelMethods(structName, receiver, tableName string, st *ast.StructType) string {
	var genCode strings.Builder
	genCode.WriteString(fmt.Sprintf("\n\n// --- Généré automatiquement pour %s ---\n\n", structName))

	var primaryKey string // Version db en miniscule
	var firstField string // Premier champ de la struct

	// Récupère le nom du premier champ pour la clé primaire
	for _, field := range st.Fields.List {
		if len(field.Names) > 0 {
			firstField = field.Names[0].Name
			tag := ""
			if field.Tag != nil {
				tag = reflectTagValue(field.Tag.Value, "db")
			}
			if tag != "" {
				primaryKey = tag
			} else {
				primaryKey = strings.ToLower(firstField)
			}
			break
		}
	}

	// Génère GetMetaData avec PrimaryKey et Table
	genCode.WriteString("// GetMetaData renvoie les informations sur le model, la table, clé primaire, etc.\n")
	genCode.WriteString(fmt.Sprintf("func (%s *%s) GetMetaData() ormx.ModelMeta {\n", receiver, structName))
	genCode.WriteString(fmt.Sprintf("\treturn ormx.ModelMeta{\n\t\tPrimaryKey: \"%s\",\n\t\tTable:      \"%s\",\n\t}\n", primaryKey, tableName))
	genCode.WriteString("}\n\n")

	// Génère la variable statique pour les colonnes
	genCode.WriteString("// Variable statique qui renvoie le nom des colonnes, cela est beaucoup plus performant.\n")
	genCode.WriteString("// IMPORTANT :\n")
	genCode.WriteString("// Les valeurs doivent être dans le même ordre que la struct et la base de données.\n")
	genCode.WriteString("// Si la base de données change, il faut mettre à jour cette variable statique.\n")
	genCode.WriteString(fmt.Sprintf("var %sColumns = []string{\n", lowerFirst(structName)))
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue
		}
		name := field.Names[0].Name
		tag := ""
		if field.Tag != nil {
			tag = reflectTagValue(field.Tag.Value, "db")
		}
		col := tag
		if col == "" {
			col = strings.ToLower(name)
		}
		genCode.WriteString(fmt.Sprintf("\t\"%s\",\n", col))
	}
	genCode.WriteString("}\n\n")

	// Génère GetColumns
	genCode.WriteString("// GetColumns renvoie les colonnes de la table de la base de données.\n")
	genCode.WriteString(fmt.Sprintf("func (%s *%s) GetColumns() []string {\n", receiver, structName))
	genCode.WriteString(fmt.Sprintf("\treturn %sColumns\n}\n\n", lowerFirst(structName)))

	// Génère GetField
	genCode.WriteString("// GetField renvoie une structure ormx.OpsFields avec toutes les infos et le pointer vers un champ.\n")
	genCode.WriteString(fmt.Sprintf("func (%s *%s) GetField(field string) *ormx.OpsFields {\n", receiver, structName))
	genCode.WriteString("\tswitch field {\n")
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue
		}
		name := field.Names[0].Name
		tag := ""
		if field.Tag != nil {
			tag = reflectTagValue(field.Tag.Value, "db")
		}
		col := tag
		if col == "" {
			col = strings.ToLower(name)
		}
		typ := "string"
		switch t := field.Type.(type) {
		case *ast.Ident:
			typ = t.Name
		case *ast.SelectorExpr:
			typ = fmt.Sprintf("%s.%s", t.X, t.Sel)
		}
		genCode.WriteString(fmt.Sprintf("\tcase \"%s\":\n", col))
		genCode.WriteString(fmt.Sprintf("\t\treturn &ormx.OpsFields{Key: \"%s\", Typ: \"%s\", Value: %s.%s, Ptr: &%s.%s}\n", col, typ, receiver, name, receiver, name))
	}
	genCode.WriteString("\tdefault:\n\t\treturn nil\n\t}\n}\n\n")

	// Génère IsNew
	genCode.WriteString("// IsNew permet de savoir s'il s'agit d'un nouvel objet.\n")
	genCode.WriteString("// Un nouvel objet à toujours la valeur de sa clé primaire vide (0).\n")
	genCode.WriteString("// Si la clé primaire n'est pas vide, cela veut dire qu'il existe dans la DB.\n")
	genCode.WriteString(fmt.Sprintf("func (%s *%s) IsNew() bool {\n", receiver, structName))
	genCode.WriteString(fmt.Sprintf("\tif %s.setNew {\n\t\treturn true\n\t}\n", receiver))
	genCode.WriteString(fmt.Sprintf("\treturn %s.%s == 0\n}\n\n", receiver, firstField))

	// Génère SetLastID
	genCode.WriteString("// SetLastID permet d'injecter le dernier ID lors d'une insertion dans la DB\n")
	genCode.WriteString(fmt.Sprintf("func (%s *%s) SetLastID(id int64) {\n", receiver, structName))
	genCode.WriteString(fmt.Sprintf("\t%s.%s = id\n}\n\n", receiver, firstField))

	// Génère GetPrimaryKeyVal
	genCode.WriteString("// GetPrimaryKeyVal utilisé pour les génériques, il permet de renvoyer la valeur de la clé primaire.\n")
	genCode.WriteString(fmt.Sprintf("func (%s *%s) GetPrimaryKeyVal() int64 {\n", receiver, structName))
	genCode.WriteString(fmt.Sprintf("\treturn %s.%s\n}\n\n", receiver, firstField))

	return genCode.String()
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
