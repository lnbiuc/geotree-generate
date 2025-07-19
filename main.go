package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TreeNode 表示树结构中的一个节点
type TreeNode struct {
	Name     string               `json:"name"`
	Children map[string]*TreeNode `json:"children,omitempty"`
	Parent   *TreeNode            `json:"-"`
}

// CategoryAnalyzer 分类分析器
type CategoryAnalyzer struct {
	dataDir        string
	categories     map[string]*TreeNode
	tree           *TreeNode
	processedFiles map[string]bool
}

// NewCategoryAnalyzer 创建新的分析器
func NewCategoryAnalyzer(dataDir string) *CategoryAnalyzer {
	return &CategoryAnalyzer{
		dataDir:        dataDir,
		categories:     make(map[string]*TreeNode),
		tree:           &TreeNode{Name: "domain-list-community", Children: make(map[string]*TreeNode)},
		processedFiles: make(map[string]bool),
	}
}

func DownloadV2RayRepoData() {
	repoURL := "https://github.com/v2ray/domain-list-community.git"
	tmpDir := "v2ray_repo_tmp"

	// 清理旧的临时目录（如果存在）
	_ = os.RemoveAll(tmpDir)

	// 1. clone 仓库
	fmt.Println("Cloning repository...")
	cmd := exec.Command("git", "clone", "--depth=1", repoURL, tmpDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = fmt.Errorf("git clone failed: %w", err)
	}

	// 2. 复制 /data 到当前目录
	srcDataPath := filepath.Join(tmpDir, "data")
	dstDataPath := "./data"

	fmt.Println("Copying data directory...")
	err := copyDir(srcDataPath, dstDataPath)
	if err != nil {
		_ = fmt.Errorf("copy data dir failed: %w", err)
	}

	// 3. 删除临时 clone 的目录
	_ = os.RemoveAll(tmpDir)

	fmt.Println("Done.")
}

func copyDir(src string, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	err = os.MkdirAll(dst, os.ModePerm)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyFile 复制单个文件
func copyFile(srcFile, dstFile string) error {
	in, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dstFile)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// ScanDataDirectory 扫描data目录
func (ca *CategoryAnalyzer) ScanDataDirectory() error {
	if _, err := os.Stat(ca.dataDir); os.IsNotExist(err) {
		return fmt.Errorf("目录不存在: %s", ca.dataDir)
	}

	err := filepath.WalkDir(ca.dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		relPath, _ := filepath.Rel(ca.dataDir, path)
		filename := strings.ReplaceAll(relPath, string(filepath.Separator), "/")
		filename = strings.TrimSuffix(filename, filepath.Ext(filename))

		node := &TreeNode{Name: filename, Children: make(map[string]*TreeNode)}
		ca.categories[filename] = node

		return nil
	})

	return err
}

// parseIncludes 解析文件中的include关系
func (ca *CategoryAnalyzer) parseIncludes(filepath string) ([]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var includes []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "include:") {
			includedFile := strings.TrimSpace(strings.TrimPrefix(line, "include:"))
			includes = append(includes, includedFile)
		}
	}

	return includes, scanner.Err()
}

// BuildTree 构建树结构
func (ca *CategoryAnalyzer) BuildTree() {
	for categoryName := range ca.categories {
		ca.processCategory(categoryName)
	}

	for _, node := range ca.categories {
		if node.Parent == nil {
			ca.tree.Children[node.Name] = node
		}
	}
}

// processCategory 处理分类的包含关系
func (ca *CategoryAnalyzer) processCategory(categoryName string) {
	if ca.processedFiles[categoryName] {
		return
	}
	ca.processedFiles[categoryName] = true

	node := ca.categories[categoryName]
	includes, err := ca.getCategoryIncludes(categoryName)
	if err != nil {
		return
	}

	for _, includedFile := range includes {
		if childNode, exists := ca.categories[includedFile]; exists {
			childNode.Parent = node
			node.Children[includedFile] = childNode
			ca.processCategory(includedFile)
		}
	}
}

// getCategoryIncludes 获取分类的包含关系
func (ca *CategoryAnalyzer) getCategoryIncludes(categoryName string) ([]string, error) {
	filepath := filepath.Join(ca.dataDir, categoryName)
	return ca.parseIncludes(filepath)
}

// PrintConsoleTree 打印控制台树结构
func (ca *CategoryAnalyzer) PrintConsoleTree() {
	fmt.Println("=== 控制台树形结构 ===")
	ca.printNode(ca.tree, -1, true)
}

// printNode 打印节点
func (ca *CategoryAnalyzer) printNode(node *TreeNode, depth int, isLast bool) {
	if depth >= 0 {
		prefix := ""
		for i := 0; i < depth; i++ {
			prefix += "│   "
		}

		if isLast {
			prefix += "└── "
		} else {
			prefix += "├── "
		}

		fmt.Printf("%s%s\n", prefix, node.Name)
	}

	var childNames []string
	for name := range node.Children {
		childNames = append(childNames, name)
	}
	sort.Strings(childNames)

	for i, name := range childNames {
		isLastChild := (i == len(childNames)-1)
		ca.printNode(node.Children[name], depth+1, isLastChild)
	}
}

// ExportJSON 导出为JSON格式
func (ca *CategoryAnalyzer) ExportJSON(filename string) error {
	jsonData, err := json.MarshalIndent(ca.tree, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return err
	}

	fmt.Printf("✅ JSON文件已保存: %s\n", filename)
	return nil
}

// ExportHTML 导出为交互式HTML页面
func (ca *CategoryAnalyzer) ExportHTML(filename string) error {
	htmlTemplate := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Domain List Community Tree</title>
    <style>
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            margin: 20px;
            background-color: #f5f5f5;
            color: #333;
        }

        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            margin: 20px;
            background-color: var(--bg-color);
            color: var(--text-color);
            transition: background-color 0.3s ease, color 0.3s ease;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        .tree {
            font-family: 'Courier New', monospace;
            line-height: 1.6;
            margin-top: 20px;
        }
        .node {
            margin: 2px 0;
            cursor: pointer;
            transition: background-color 0.2s;
            padding: 2px 4px;
            display: flex;
            align-items: center;
            justify-content: space-between;
        }
        .node:hover {
            background-color: #e3f2fd;
            border-radius: 4px;
        }
        .node-content {
            flex: 1;
            cursor: pointer;
        }
        .node.collapsible .node-content:hover {
            text-decoration: underline;
        }
        .view-source-btn {
            padding: 2px 8px;
            background: #28a745;
            color: white;
            border: none;
            border-radius: 3px;
            font-size: 11px;
            cursor: pointer;
            margin-left: 10px;
            text-decoration: none;
            display: inline-block;
            opacity: 0.7;
            transition: opacity 0.2s ease;
        }
        .view-source-btn:hover {
            opacity: 1;
            background: #218838;
        }
        .node.category { color: #7b1fa2; font-weight: bold; }
        .node.company { color: #2e7d32; }
        .node.geo { color: #f57c00; }
        .node.service { color: #1976d2; }
        .collapsible {
            position: relative;
        }
        .collapsible:before {
            content: '▼';
            position: absolute;
            left: -15px;
            color: #666;
            font-size: 10px;
        }
        .collapsible.collapsed:before {
            content: '▶';
        }
        .children {
            margin-left: 20px;
        }
        .children.hidden {
            display: none;
        }
        .header {
            text-align: center;
            margin-bottom: 30px;
        }
        .stats {
            background: #e3f2fd;
            padding: 15px;
            border-radius: 6px;
            margin-bottom: 20px;
        }
        .controls {
            display: flex;
            gap: 10px;
            justify-content: center;
            margin-bottom: 20px;
            flex-wrap: wrap;
        }
        .btn {
            padding: 10px 20px;
            background: #007bff;
            color: white;
            border: none;
            border-radius: 5px;
            cursor: pointer;
            font-size: 14px;
            transition: background-color 0.2s ease;
        }
        .btn:hover {
            background: #0056b3;
        }
        .btn:active {
            transform: translateY(1px);
        }
        @media (max-width: 768px) {
            body {
                margin: 10px;
            }
            .container {
                padding: 15px;
            }
            .controls {
                flex-direction: column;
                align-items: center;
            }
            .btn {
                width: 100%;
                max-width: 200px;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🌳 Domain List Community Tree</h1>
			<p>更新时间：{{.UpdateAt}} | 每周更新1次</p>
        </div>
        
        <div class="controls">
            <button class="btn" id="expandAllBtn">📂 展开全部</button>
            <button class="btn" id="collapseAllBtn">📁 收起全部</button>
        </div>
        
        <div class="tree" id="tree">
            {{.TreeHTML}}
        </div>
    </div>

    <script>
        // 折叠/展开功能
        document.addEventListener('click', function(e) {
            // 如果点击的是源码按钮，不执行展开/收起逻辑
            if (e.target.classList.contains('view-source-btn')) {
                return;
            }
            
            // 查找最近的可折叠节点
            let targetNode = e.target;
            
            // 如果点击的是node-content，找到它的父节点
            if (e.target.classList.contains('node-content')) {
                targetNode = e.target.parentElement;
            }
            
            // 如果点击的节点本身就是collapsible，或者它的父节点是collapsible
            if (targetNode.classList.contains('collapsible')) {
                e.preventDefault();
                e.stopPropagation();
                
                targetNode.classList.toggle('collapsed');
                const children = targetNode.nextElementSibling;
                if (children && children.classList.contains('children')) {
                    children.classList.toggle('hidden');
                }
            }
        });

        // 展开全部功能
        document.getElementById('expandAllBtn').addEventListener('click', function() {
            const collapsibleNodes = document.querySelectorAll('.collapsible');
            const childrenNodes = document.querySelectorAll('.children');
            
            collapsibleNodes.forEach(node => {
                node.classList.remove('collapsed');
            });
            
            childrenNodes.forEach(children => {
                children.classList.remove('hidden');
            });
        });

        // 收起全部功能 - 只保留一级分类
        document.getElementById('collapseAllBtn').addEventListener('click', function() {
            // 收起所有节点
            const allCollapsibleNodes = document.querySelectorAll('.collapsible');
            const allChildrenNodes = document.querySelectorAll('.children');
            
            allCollapsibleNodes.forEach(node => {
                node.classList.add('collapsed');
            });
            
            allChildrenNodes.forEach(children => {
                children.classList.add('hidden');
            });
        });

        // 初始化：展开第一层
        document.querySelectorAll('.tree > .node.collapsible').forEach(node => {
            node.classList.remove('collapsed');
            const children = node.nextElementSibling;
            if (children) children.classList.remove('hidden');
        });
    </script>
</body>
</html>`

	treeHTML := ca.generateHTMLTree(ca.tree, 0)
	totalCategories := len(ca.categories)

	tmpl, err := template.New("html").Parse(htmlTemplate)
	if err != nil {
		return err
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	loc, _ := time.LoadLocation("Asia/Shanghai") // 东八区时区对象
	now := time.Now().In(loc)                    // 转换为东八区时间

	err = tmpl.Execute(file, struct {
		TreeHTML        template.HTML
		TotalCategories int
		UpdateAt        string
	}{
		TreeHTML:        template.HTML(treeHTML),
		TotalCategories: totalCategories,
		UpdateAt:        now.Format("2006-01-02 15:04:05"),
	})

	if err != nil {
		return err
	}

	fmt.Printf("✅ HTML文件已保存: %s\n", filename)
	fmt.Printf("   在浏览器中打开查看交互式树结构\n")
	return nil
}

// generateHTMLTree 生成HTML树结构
func (ca *CategoryAnalyzer) generateHTMLTree(node *TreeNode, depth int) string {
	var sb strings.Builder

	if node.Name != "domain-list-community" {
		class := ca.getNodeClass(node.Name)
		hasChildren := len(node.Children) > 0

		// 构建节点内容
		nodeContent := fmt.Sprintf(`<span class="node-content">%s</span>`, node.Name)

		// 添加查看源码按钮
		sourceButton := fmt.Sprintf(`<a href="https://raw.githubusercontent.com/v2ray/domain-list-community/refs/heads/master/data/%s" target="_blank" class="view-source-btn" onclick="event.stopPropagation()">Github Source</a>`, node.Name)

		if hasChildren {
			sb.WriteString(fmt.Sprintf(`<div class="node collapsible %s">%s%s</div>`, class, nodeContent, sourceButton))
			sb.WriteString(`<div class="children hidden">`)
		} else {
			sb.WriteString(fmt.Sprintf(`<div class="node %s">%s%s</div>`, class, nodeContent, sourceButton))
		}

		var childNames []string
		for name := range node.Children {
			childNames = append(childNames, name)
		}
		sort.Strings(childNames)

		for _, childName := range childNames {
			sb.WriteString(ca.generateHTMLTree(node.Children[childName], depth+1))
		}

		if hasChildren {
			sb.WriteString(`</div>`)
		}
	} else {
		var childNames []string
		for name := range node.Children {
			childNames = append(childNames, name)
		}
		sort.Strings(childNames)

		for _, childName := range childNames {
			sb.WriteString(ca.generateHTMLTree(node.Children[childName], depth))
		}
	}

	return sb.String()
}

// getNodeClass 获取节点CSS类
func (ca *CategoryAnalyzer) getNodeClass(name string) string {
	if strings.HasPrefix(name, "category-") {
		return "category"
	} else if isCompany(name) {
		return "company"
	} else if strings.HasPrefix(name, "geo") || isCountry(name) {
		return "geo"
	}
	return "service"
}

// 辅助函数
func isCompany(name string) bool {
	companies := []string{"google", "microsoft", "apple", "facebook", "amazon", "netflix", "github", "gitlab", "twitter", "youtube", "instagram", "tiktok", "zoom", "discord", "spotify", "openai", "alibaba", "baidu", "tencent", "douban", "weibo", "bilibili"}
	for _, company := range companies {
		if strings.Contains(name, company) {
			return true
		}
	}
	return false
}

func isCountry(name string) bool {
	countries := []string{"cn", "us", "jp", "kr", "hk", "tw", "uk", "de", "fr", "ru"}
	return contains(countries, name)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func main() {
	dataDir := "./data"

	fmt.Println("🌳 Domain List Community 多格式可视化工具")
	fmt.Println(strings.Repeat("=", 50))

	analyzer := NewCategoryAnalyzer(dataDir)

	if err := analyzer.ScanDataDirectory(); err != nil {
		DownloadV2RayRepoData()
		//fmt.Printf("错误: %v\n", err)
		//os.Exit(1)
	}

	analyzer.BuildTree()

	// 1. 控制台输出
	analyzer.PrintConsoleTree()

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("📤 正在生成多种格式的输出文件...")

	// 2. JSON格式
	if err := analyzer.ExportJSON("domain_tree.json"); err != nil {
		fmt.Printf("❌ JSON导出失败: %v\n", err)
	}

	// 4. 交互式HTML
	if err := analyzer.ExportHTML("domain_tree.html"); err != nil {
		fmt.Printf("❌ HTML导出失败: %v\n", err)
	}

	fmt.Println("\n✨ 完成！生成的文件:")
	fmt.Println("   📄 domain_tree.json  - JSON数据格式")
	fmt.Println("   🌐 domain_tree.html  - 交互式网页")
}
