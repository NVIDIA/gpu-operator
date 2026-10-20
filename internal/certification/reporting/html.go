/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package reporting

import (
	"bytes"
	"html/template"

	"github.com/NVIDIA/gpu-operator/internal/certification"
)

var reportTemplate = template.Must(template.New("report").Parse(htmlTemplateStr))

// GenerateHTMLReport generates an HTML report from the suite results.
func GenerateHTMLReport(results *certification.SuiteResult) (string, error) {
	var buf bytes.Buffer
	if err := reportTemplate.Execute(&buf, results); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const htmlTemplateStr = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>GPU Operator Certification Report</title>
    <style>
        :root {
            --bg-primary: #0f1419;
            --bg-secondary: #1a1f2e;
            --bg-card: #232b3b;
            --text-primary: #e6edf3;
            --text-secondary: #8b949e;
            --accent-green: #76b900;
            --accent-red: #f85149;
            --accent-yellow: #d29922;
            --accent-blue: #58a6ff;
            --border-color: #30363d;
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: var(--bg-primary);
            color: var(--text-primary);
            line-height: 1.6;
            min-height: 100vh;
        }
        .container { max-width: 1200px; margin: 0 auto; padding: 2rem; }
        header {
            text-align: center;
            margin-bottom: 3rem;
            padding: 2rem;
            background: linear-gradient(135deg, var(--bg-secondary) 0%, var(--bg-card) 100%);
            border-radius: 12px;
            border: 1px solid var(--border-color);
        }
        .logo { font-size: 2.5rem; font-weight: 700; color: var(--accent-green); margin-bottom: 0.5rem; }
        .subtitle { color: var(--text-secondary); font-size: 1.1rem; }
        .summary-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1.5rem; margin-bottom: 2rem; }
        .summary-card {
            background: var(--bg-card);
            border-radius: 12px;
            padding: 1.5rem;
            text-align: center;
            border: 1px solid var(--border-color);
            transition: transform 0.2s, box-shadow 0.2s;
        }
        .summary-card:hover { transform: translateY(-2px); box-shadow: 0 8px 25px rgba(0,0,0,0.3); }
        .summary-card .number { font-size: 3rem; font-weight: 700; line-height: 1; }
        .summary-card .label { color: var(--text-secondary); font-size: 0.9rem; text-transform: uppercase; letter-spacing: 1px; margin-top: 0.5rem; }
        .summary-card.passed .number { color: var(--accent-green); }
        .summary-card.failed .number { color: var(--accent-red); }
        .summary-card.skipped .number { color: var(--accent-yellow); }
        .summary-card.total .number { color: var(--accent-blue); }
        .meta-info { display: flex; justify-content: center; gap: 2rem; margin-bottom: 2rem; color: var(--text-secondary); font-size: 0.9rem; }
        .tests-section { margin-top: 2rem; }
        .section-title { font-size: 1.5rem; margin-bottom: 1.5rem; border-bottom: 2px solid var(--accent-green); padding-bottom: 0.5rem; display: inline-block; }
        .test-card { background: var(--bg-card); border-radius: 12px; margin-bottom: 1rem; border: 1px solid var(--border-color); overflow: hidden; }
        .test-header { display: flex; justify-content: space-between; align-items: center; padding: 1.25rem 1.5rem; cursor: pointer; transition: background 0.2s; }
        .test-header:hover { background: rgba(255,255,255,0.02); }
        .test-name { font-size: 1.1rem; font-weight: 600; display: flex; align-items: center; gap: 0.75rem; }
        .status-badge { padding: 0.25rem 0.75rem; border-radius: 20px; font-size: 0.8rem; font-weight: 600; text-transform: uppercase; }
        .status-badge.passed { background: rgba(118, 185, 0, 0.15); color: var(--accent-green); }
        .status-badge.failed { background: rgba(248, 81, 73, 0.15); color: var(--accent-red); }
        .status-badge.skipped { background: rgba(210, 153, 34, 0.15); color: var(--accent-yellow); }
        .test-meta { display: flex; gap: 1.5rem; color: var(--text-secondary); font-size: 0.85rem; }
        .test-details { padding: 1rem 1.5rem 1.5rem; border-top: 1px solid var(--border-color); }
        .test-details.hidden { display: none; }
        .details-list { background: var(--bg-secondary); border-radius: 8px; padding: 1rem; margin-bottom: 1rem; font-family: monospace; font-size: 0.85rem; max-height: 300px; overflow-y: auto; }
        .detail-item { color: var(--text-secondary); padding: 0.15rem 0; }
        .detail-meta { display: flex; gap: 2rem; color: var(--text-secondary); font-size: 0.85rem; margin-top: 0.5rem; }
        .error-message { background: rgba(248, 81, 73, 0.1); border: 1px solid rgba(248, 81, 73, 0.3); border-radius: 8px; padding: 1rem; margin-bottom: 1rem; color: var(--accent-red); }
        .chevron { transition: transform 0.2s; color: var(--text-secondary); }
        .chevron.open { transform: rotate(90deg); }
        .platform-section {
            background: var(--bg-card);
            border-radius: 12px;
            border: 1px solid var(--border-color);
            margin-bottom: 2rem;
        }
        .platform-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 1rem 1.5rem;
            cursor: pointer;
            transition: background 0.2s;
            user-select: none;
        }
        .platform-header:hover { background: rgba(255,255,255,0.02); }
        .platform-title { font-size: 1.1rem; font-weight: 600; display: flex; align-items: center; gap: 0.75rem; }
        .platform-body { padding: 0 1.5rem 1.5rem; }
        .platform-body.hidden { display: none; }
        .platform-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 1rem; margin-bottom: 1rem; }
        .platform-item { display: flex; flex-direction: column; gap: 0.15rem; }
        .platform-item .label { color: var(--text-secondary); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.5px; }
        .platform-item .value { font-size: 0.95rem; }
        .node-table { width: 100%; border-collapse: collapse; font-size: 0.85rem; margin-bottom: 1rem; }
        .node-table th { text-align: left; padding: 0.5rem 0.75rem; border-bottom: 1px solid var(--border-color); color: var(--text-secondary); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.5px; }
        .node-table td { padding: 0.5rem 0.75rem; border-bottom: 1px solid rgba(48,54,61,0.5); }
        .smi-output { background: var(--bg-secondary); border-radius: 8px; padding: 1rem; font-family: monospace; font-size: 0.8rem; max-height: 300px; overflow-y: auto; white-space: pre; color: var(--text-secondary); line-height: 1.4; }
        .platform-sub { font-size: 0.9rem; font-weight: 600; margin: 1rem 0 0.5rem; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.5px; }
        footer { text-align: center; margin-top: 3rem; padding: 2rem; color: var(--text-secondary); font-size: 0.85rem; }
        .download-link { color: var(--accent-blue); text-decoration: none; display: inline-block; margin-bottom: 1rem; padding: 0.5rem 1rem; border: 1px solid var(--accent-blue); border-radius: 6px; transition: background 0.2s; }
        .download-link:hover { background: rgba(88, 166, 255, 0.1); }
    </style>
</head> 
<body>
    <div class="container">
        <header>
            <div class="logo">NVIDIA GPU Operator</div>
            <div class="subtitle">Self-Certification Report</div>
        </header>
        <div class="summary-grid">
            <div class="summary-card total"><div class="number">{{.Summary.Total}}</div><div class="label">Total Tests</div></div>
            <div class="summary-card passed"><div class="number">{{.Summary.Passed}}</div><div class="label">Passed</div></div>
            <div class="summary-card failed"><div class="number">{{.Summary.Failed}}</div><div class="label">Failed</div></div>
            <div class="summary-card skipped"><div class="number">{{.Summary.Skipped}}</div><div class="label">Skipped</div></div>
        </div>
        <div class="meta-info">
            <span>🕐 Started: {{.StartTime.Format "2006-01-02 15:04:05 UTC"}}</span>
            <span>⏱️ Duration: {{.Duration}}</span>
        </div>
        {{if .Environment}}
        <div class="platform-section">
            <div class="platform-header" onclick="togglePlatform(this)">
                <div class="platform-title">
                    <span class="chevron">▶</span>
                    Platform Details
                </div>
                <div style="color: var(--text-secondary); font-size: 0.85rem;">
                    {{.Environment.Operator.Version}} &middot; K8s {{.Environment.Cluster.KubernetesVersion}} &middot; {{.Environment.Cluster.GPUNodeCount}} GPU node{{if ne .Environment.Cluster.GPUNodeCount 1}}s{{end}}
                </div>
            </div>
            <div class="platform-body hidden">
                <div class="platform-grid">
                    <div class="platform-item"><span class="label">GPU Operator Version</span><span class="value">{{.Environment.Operator.Version}}</span></div>
                    <div class="platform-item"><span class="label">Operator Image</span><span class="value">{{.Environment.Operator.Image}}</span></div>
                    <div class="platform-item"><span class="label">Kubernetes Version</span><span class="value">{{.Environment.Cluster.KubernetesVersion}}</span></div>
                    <div class="platform-item"><span class="label">Platform</span><span class="value">{{.Environment.Cluster.Platform}}</span></div>
                    <div class="platform-item"><span class="label">Total Nodes</span><span class="value">{{.Environment.Cluster.TotalNodes}}</span></div>
                    <div class="platform-item"><span class="label">GPU Nodes</span><span class="value">{{.Environment.Cluster.GPUNodeCount}}</span></div>
                </div>
                {{if .Environment.Nodes}}
                <div class="platform-sub">Nodes</div>
                <div style="overflow-x: auto;">
                <table class="node-table">
                    <thead><tr>
                        <th>Name</th><th>Status</th><th>Roles</th><th>OS Image</th><th>Container Runtime</th><th>Kubelet</th><th>GPU</th><th>GPU Count</th><th>Driver</th>
                    </tr></thead>
                    <tbody>
                    {{range .Environment.Nodes}}
                    <tr>
                        <td>{{.Name}}</td>
                        <td>{{.Status}}</td>
                        <td>{{.Roles}}</td>
                        <td>{{.OSImage}}</td>
                        <td>{{.ContainerRuntime}}</td>
                        <td>{{.KubeletVersion}}</td>
                        <td>{{.GPUProduct}}</td>
                        <td>{{.GPUCount}}</td>
                        <td>{{.DriverVersion}}</td>
                    </tr>
                    {{end}}
                    </tbody>
                </table>
                </div>
                {{end}}
                {{if .Environment.Cluster.NvidiaSmiOutput}}
                <div class="platform-sub">nvidia-smi</div>
                <div class="smi-output">{{range .Environment.Cluster.NvidiaSmiOutput}}{{.}}
{{end}}</div>
                {{end}}
                <div style="margin-top: 1rem; color: var(--text-secondary); font-size: 0.8rem; font-style: italic;">See the full environment snapshot in <a href="results.json" style="color: var(--accent-blue);">results.json</a></div>
            </div>
        </div>
        {{end}}
        <div class="tests-section">
            <h2 class="section-title">Test Results</h2>
            {{range .TestSets}}
            <div class="test-card">
                <div class="test-header" onclick="toggleDetails(this)">
                    <div class="test-name">
                        <span class="chevron">▶</span>
                        [{{.ID}}] {{.Description}}
                        <span class="status-badge {{.Status}}">{{.Status}}</span>
                    </div>
                    <div class="test-meta">
                        <span>Duration: {{.Duration}}</span>
                    </div>
                </div>
                <div class="test-details hidden">
                    {{if .Error}}<div class="error-message"><strong>Error:</strong> {{.Error}}</div>{{end}}
                    {{if .Details}}<div class="details-list">{{range .Details}}<div class="detail-item">{{.}}</div>{{end}}</div>{{end}}
                    <div class="detail-meta">
                        <span><strong>Started:</strong> {{.StartTime.Format "15:04:05"}}</span>
                        <span><strong>Duration:</strong> {{.Duration}}</span>
                    </div>
                </div>
            </div>
            {{end}}
        </div>
        <footer>
            Generated by GPU Operator Self-Certification Tool<br>
            Report generated at {{.EndTime.Format "2006-01-02 15:04:05 UTC"}}
        </footer>
    </div>
    <script>
        function togglePlatform(header) {
            const body = header.nextElementSibling;
            const chevron = header.querySelector('.chevron');
            body.classList.toggle('hidden');
            chevron.classList.toggle('open');
        }
        function toggleDetails(header) {
            const details = header.nextElementSibling;
            const chevron = header.querySelector('.chevron');
            details.classList.toggle('hidden');
            chevron.classList.toggle('open');
        }
        document.querySelectorAll('.status-badge.failed').forEach(badge => {
            const header = badge.closest('.test-header');
            if (header) toggleDetails(header);
        });
    </script>
</body>
</html>
`
