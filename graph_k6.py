#!/usr/bin/env python3
"""Parse k6 NDJSON results and produce an interactive HTML chart report."""

import json
import math
from collections import defaultdict
from pathlib import Path

INPUT  = Path(__file__).parent / "k6_results.json"
OUTPUT = Path(__file__).parent / "k6_charts.html"

BUCKET_SECS = 30

LATENCY_METRICS = ["cd_latency", "upload_latency", "download_latency",
                   "ls_latency", "tree_latency", "delete_latency"]
LATENCY_LABELS  = {"cd_latency":"CD","upload_latency":"Upload",
                   "download_latency":"Download","ls_latency":"List",
                   "tree_latency":"Tree","delete_latency":"Delete"}
LATENCY_COLORS  = {"cd_latency":"rgb(54,162,235)","upload_latency":"rgb(255,99,132)",
                   "download_latency":"rgb(75,192,192)","ls_latency":"rgb(255,205,86)",
                   "tree_latency":"rgb(153,102,255)","delete_latency":"rgb(255,159,64)"}

SCENARIO_ORDER = [
    "setup_seed","steady_classroom","burst_download","integrity_check",
    "upload_delete_cycle","teacher_race","permission_test","large_file",
    "rapid_fire","spike_test","soak_test","tree_stress",
    "session_conflict_upload","session_conflict_navigate",
    "mkdirWorkflow","renameDirWorkflow","renameFileWorkflow",
    "concurrentRenameStress","concurrentCdRaceStress",
]

# grpc methods we want per-scenario latency bars for
GRPC_METHODS = ["ChangeDirectory","ListDirectory","Download","Upload","Delete",
                "MakeDirectory","Rename","RenameDirectory","TreeDirectory","CurrentDirectory"]

def bucket_ts(ts_str):
    import datetime
    ts_str = ts_str[:26] + ts_str[26:].replace(":", "")
    for fmt in ("%Y-%m-%dT%H:%M:%S.%f%z", "%Y-%m-%dT%H:%M:%S%z"):
        try:
            dt = datetime.datetime.strptime(ts_str, fmt)
            return (int(dt.timestamp()) // BUCKET_SECS) * BUCKET_SECS
        except ValueError:
            continue
    return 0

def pct(values, p):
    if not values: return 0
    s = sorted(values)
    return round(s[max(0, int(math.ceil(p/100*len(s)))-1)], 1)

def avg(values):
    return round(sum(values)/len(values), 1) if values else 0

# ── parse ─────────────────────────────────────────────────────────────────────
print(f"Parsing {INPUT} …")
points = []
with open(INPUT) as f:
    for line in f:
        line = line.strip()
        if not line: continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
            continue
        if obj.get("type") != "Point": continue
        data = obj.get("data", {})
        points.append({"metric": obj["metric"],
                       "value":  data.get("value", 0),
                       "time":   data.get("time", ""),
                       "tags":   data.get("tags", {})})
print(f"  {len(points):,} data points loaded")

# ── bucket accumulators ───────────────────────────────────────────────────────
latency_buckets   = defaultdict(lambda: defaultdict(list))  # metric -> bucket -> [ms]
error_buckets     = defaultdict(int)
vus_buckets       = defaultdict(int)
success_buckets   = defaultdict(lambda: [0, 0])             # bucket -> [sum, count]
iter_buckets      = defaultdict(int)                         # bucket -> count
dropped_buckets   = defaultdict(int)
data_sent_buckets = defaultdict(float)                       # bucket -> bytes
data_recv_buckets = defaultdict(float)
streams_buckets   = defaultdict(int)
integrity_buckets = defaultdict(int)
checks_buckets    = defaultdict(lambda: [0, 0])             # bucket -> [pass, total]

# per-scenario accumulators
scenario_grpc     = defaultdict(lambda: defaultdict(list))  # scenario -> method -> [ms]
scenario_iter_dur = defaultdict(list)                        # scenario -> [ms]
scenario_dropped  = defaultdict(int)                         # scenario -> count
scenario_checks   = defaultdict(lambda: [0, 0])             # scenario -> [pass, total]
scenario_latency  = defaultdict(lambda: defaultdict(list))  # scenario -> metric -> [ms]
all_scenarios_seen = set()

integrity_total   = 0
perm_bypass_total = 0
total_iterations  = 0

for p in points:
    metric  = p["metric"]
    value   = p["value"]
    b       = bucket_ts(p["time"])
    tags    = p["tags"]
    scenario = tags.get("scenario", "")

    if scenario:
        all_scenarios_seen.add(scenario)

    if metric in LATENCY_METRICS:
        latency_buckets[metric][b].append(value)
        if scenario:
            scenario_latency[scenario][metric].append(value)

    elif metric == "rpc_errors":
        error_buckets[b] += value

    elif metric == "vus":
        vus_buckets[b] = max(vus_buckets[b], value)

    elif metric == "success_rate":
        success_buckets[b][0] += value
        success_buckets[b][1] += 1

    elif metric == "iterations":
        iter_buckets[b] += value
        total_iterations += value

    elif metric == "dropped_iterations":
        dropped_buckets[b] += value
        if scenario:
            scenario_dropped[scenario] += value

    elif metric == "data_sent":
        data_sent_buckets[b] += value

    elif metric == "data_received":
        data_recv_buckets[b] += value

    elif metric == "grpc_streams":
        streams_buckets[b] += value

    elif metric == "integrity_failures":
        integrity_buckets[b] += value
        integrity_total += value

    elif metric == "permission_bypass":
        perm_bypass_total += value

    elif metric == "checks":
        checks_buckets[b][0] += value          # 1=pass 0=fail
        checks_buckets[b][1] += 1
        if scenario:
            scenario_checks[scenario][0] += value
            scenario_checks[scenario][1] += 1

    elif metric == "iteration_duration":
        if scenario:
            scenario_iter_dur[scenario].append(value)

    elif metric == "grpc_req_duration":
        method = tags.get("method", "")
        if scenario and method in GRPC_METHODS:
            scenario_grpc[scenario][method].append(value)

# ── time axis ─────────────────────────────────────────────────────────────────
all_buckets = sorted(set(
    list(vus_buckets) + list(error_buckets) +
    [b for m in latency_buckets.values() for b in m]
))
if not all_buckets:
    raise SystemExit("No bucketed data found")

t0 = all_buckets[0]
time_labels = [f"{(b-t0)//60}m{(b-t0)%60:02d}s" for b in all_buckets]

# ── time-series arrays ────────────────────────────────────────────────────────
latency_p95 = {m: [pct(latency_buckets[m].get(b,[]), 95) for b in all_buckets]
               for m in LATENCY_METRICS}
latency_avg  = {m: [avg(latency_buckets[m].get(b,[]))     for b in all_buckets]
               for m in LATENCY_METRICS}

errors_series   = [error_buckets.get(b, 0)   for b in all_buckets]
vus_series      = [vus_buckets.get(b, 0)     for b in all_buckets]
iter_series     = [iter_buckets.get(b, 0)    for b in all_buckets]
dropped_series  = [dropped_buckets.get(b, 0) for b in all_buckets]
integrity_series= [integrity_buckets.get(b, 0) for b in all_buckets]
streams_series  = [streams_buckets.get(b, 0) for b in all_buckets]

# bytes -> KB/s
sent_series = [round(data_sent_buckets.get(b,0)/1024/BUCKET_SECS, 1) for b in all_buckets]
recv_series = [round(data_recv_buckets.get(b,0)/1024/BUCKET_SECS, 1) for b in all_buckets]

success_series = [
    round(success_buckets[b][0]/success_buckets[b][1]*100, 1)
    if b in success_buckets and success_buckets[b][1] > 0 else None
    for b in all_buckets
]
check_pass_series = [
    round(checks_buckets[b][0]/checks_buckets[b][1]*100, 1)
    if b in checks_buckets and checks_buckets[b][1] > 0 else None
    for b in all_buckets
]

# ── scenario lists ────────────────────────────────────────────────────────────
scenarios_present = [s for s in SCENARIO_ORDER if s in all_scenarios_seen] + \
                    sorted(s for s in all_scenarios_seen if s not in SCENARIO_ORDER)

def scen_grpc_bars(method):
    p50 = [pct(scenario_grpc[s].get(method,[]), 50) for s in scenarios_present]
    p95 = [pct(scenario_grpc[s].get(method,[]), 95) for s in scenarios_present]
    return p50, p95

def scen_lat_bars(metric):
    p50 = [pct(scenario_latency[s].get(metric,[]), 50) for s in scenarios_present]
    p95 = [pct(scenario_latency[s].get(metric,[]), 95) for s in scenarios_present]
    return p50, p95

# per-scenario: iter duration p50/p95, dropped count, check pass %
iter_dur_p50 = [pct(scenario_iter_dur.get(s,[]), 50) for s in scenarios_present]
iter_dur_p95 = [pct(scenario_iter_dur.get(s,[]), 95) for s in scenarios_present]
dropped_scen = [scenario_dropped.get(s, 0) for s in scenarios_present]
check_pct_scen = [
    round(scenario_checks[s][0]/scenario_checks[s][1]*100, 1)
    if s in scenario_checks and scenario_checks[s][1] > 0 else 0
    for s in scenarios_present
]

# latency time-series datasets
def js(v): return json.dumps(v)

latency_datasets_p95 = [
    {"label": LATENCY_LABELS[m]+" p95",
     "data": latency_p95[m],
     "borderColor": LATENCY_COLORS[m],
     "backgroundColor": LATENCY_COLORS[m].replace("rgb","rgba").replace(")",",0.1)"),
     "tension":0.3,"fill":False,"pointRadius":1}
    for m in LATENCY_METRICS if any(latency_p95[m])
]

scenario_labels_js = js(scenarios_present)

# method bars
cd_p50,   cd_p95   = scen_grpc_bars("ChangeDirectory")
ls_p50,   ls_p95   = scen_grpc_bars("ListDirectory")
dl_p50,   dl_p95   = scen_lat_bars("download_latency")
ul_p50,   ul_p95   = scen_lat_bars("upload_latency")
del_p50,  del_p95  = scen_lat_bars("delete_latency")
tree_p50, tree_p95 = scen_grpc_bars("TreeDirectory")
mkdir_p50,mkdir_p95= scen_grpc_bars("MakeDirectory")
ren_p50,  ren_p95  = scen_grpc_bars("Rename")
rend_p50, rend_p95 = scen_grpc_bars("RenameDirectory")
cur_p50,  cur_p95  = scen_grpc_bars("CurrentDirectory")

total_dropped = int(sum(dropped_series))
total_checks_pass = sum(checks_buckets[b][0] for b in checks_buckets)
total_checks_all  = sum(checks_buckets[b][1] for b in checks_buckets)
check_overall_pct = round(total_checks_pass/total_checks_all*100,1) if total_checks_all else 0

# ── HTML ──────────────────────────────────────────────────────────────────────
HTML = f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>k6 Load Test Results — NEUDFS</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
<style>
  *    {{ box-sizing:border-box; }}
  body {{ font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;
         background:#0f1117;color:#e0e0e0;margin:0;padding:20px; }}
  h1   {{ color:#fff;margin-bottom:4px; }}
  .sub {{ color:#888;font-size:.9rem;margin-bottom:20px; }}
  .grid{{ display:grid;grid-template-columns:1fr 1fr;gap:18px; }}
  .card{{ background:#1a1d27;border-radius:10px;padding:18px;
          box-shadow:0 2px 8px rgba(0,0,0,.4); }}
  .full{{ grid-column:1/-1; }}
  h2   {{ margin:0 0 12px;font-size:.85rem;color:#aaa;text-transform:uppercase;
          letter-spacing:.06em; }}
  .stats{{ display:flex;gap:16px;flex-wrap:wrap;margin-bottom:20px; }}
  .stat{{ background:#23263a;border-radius:8px;padding:10px 18px; }}
  .stat .val{{ font-size:1.5rem;font-weight:700; }}
  .stat .lbl{{ font-size:.72rem;color:#888;margin-top:2px; }}
  .ok {{ color:#4ade80; }} .warn{{ color:#facc15; }} .err{{ color:#f87171; }}
  .section-title{{ color:#666;font-size:.7rem;text-transform:uppercase;
                   letter-spacing:.1em;margin:28px 0 10px;border-top:1px solid #23263a;
                   padding-top:16px; }}
</style>
</head>
<body>
<h1>k6 Load Test — NEUDFS</h1>
<p class="sub">k6_results.json &nbsp;·&nbsp; {BUCKET_SECS}s buckets &nbsp;·&nbsp;
  {len(points):,} data points &nbsp;·&nbsp; {len(scenarios_present)} scenarios &nbsp;·&nbsp;
  {int(total_iterations):,} total iterations</p>

<div class="stats">
  <div class="stat"><div class="val {'err' if integrity_total>0 else 'ok'}">{int(integrity_total)}</div><div class="lbl">Integrity Failures</div></div>
  <div class="stat"><div class="val {'err' if perm_bypass_total>0 else 'ok'}">{int(perm_bypass_total)}</div><div class="lbl">Permission Bypasses</div></div>
  <div class="stat"><div class="val warn">{int(sum(errors_series))}</div><div class="lbl">Total RPC Errors</div></div>
  <div class="stat"><div class="val {'err' if total_dropped>0 else 'ok'}">{total_dropped}</div><div class="lbl">Dropped Iterations</div></div>
  <div class="stat"><div class="val {'ok' if check_overall_pct>=90 else 'warn'}">{check_overall_pct}%</div><div class="lbl">Check Pass Rate</div></div>
  <div class="stat"><div class="val">{max(vus_series)}</div><div class="lbl">Peak VUs</div></div>
  <div class="stat"><div class="val">{max(sent_series):.0f} KB/s</div><div class="lbl">Peak Data Sent</div></div>
  <div class="stat"><div class="val">{max(recv_series):.0f} KB/s</div><div class="lbl">Peak Data Recv</div></div>
</div>

<!-- ══ TIME SERIES ══════════════════════════════════════════════════════════ -->
<p class="section-title">Time Series</p>
<div class="grid">

  <div class="card full">
    <h2>Latency p95 over time (ms)</h2>
    <canvas id="latChart" height="80"></canvas>
  </div>

  <div class="card">
    <h2>Throughput — iterations &amp; dropped per {BUCKET_SECS}s</h2>
    <canvas id="throughputChart"></canvas>
  </div>

  <div class="card">
    <h2>RPC Errors per {BUCKET_SECS}s</h2>
    <canvas id="errChart"></canvas>
  </div>

  <div class="card">
    <h2>VUs &amp; Success Rate</h2>
    <canvas id="vuChart"></canvas>
  </div>

  <div class="card">
    <h2>Check Pass Rate over time</h2>
    <canvas id="checkTimeChart"></canvas>
  </div>

  <div class="card">
    <h2>Network Throughput (KB/s)</h2>
    <canvas id="netChart"></canvas>
  </div>

  <div class="card">
    <h2>gRPC Streams opened per {BUCKET_SECS}s</h2>
    <canvas id="streamsChart"></canvas>
  </div>

  <div class="card">
    <h2>Integrity Failures over time</h2>
    <canvas id="integrityTimeChart"></canvas>
  </div>

</div>

<!-- ══ PER-SCENARIO ═════════════════════════════════════════════════════════ -->
<p class="section-title">Per-Scenario Breakdown</p>
<div class="grid">

  <div class="card full">
    <h2>Iteration Duration per Scenario (ms) — p50 / p95</h2>
    <canvas id="iterDurChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>Dropped Iterations per Scenario</h2>
    <canvas id="droppedScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>Check Pass % per Scenario</h2>
    <canvas id="checkScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>ChangeDirectory Latency per Scenario (ms) — p50 / p95</h2>
    <canvas id="cdScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>ListDirectory Latency per Scenario (ms) — p50 / p95</h2>
    <canvas id="lsScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>Upload Latency per Scenario (ms) — p50 / p95</h2>
    <canvas id="ulScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>Download Latency per Scenario (ms) — p50 / p95</h2>
    <canvas id="dlScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>Delete Latency per Scenario (ms) — p50 / p95</h2>
    <canvas id="delScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>MakeDirectory Latency per Scenario (ms) — p50 / p95</h2>
    <canvas id="mkdirScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>Rename File Latency per Scenario (ms) — p50 / p95</h2>
    <canvas id="renScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>Rename Directory Latency per Scenario (ms) — p50 / p95</h2>
    <canvas id="rendScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>TreeDirectory Latency per Scenario (ms) — p50 / p95</h2>
    <canvas id="treeScenChart" height="55"></canvas>
  </div>

  <div class="card full">
    <h2>CurrentDirectory Latency per Scenario (ms) — p50 / p95</h2>
    <canvas id="curScenChart" height="55"></canvas>
  </div>

</div>

<script>
const labels = {js(time_labels)};
const scenLabels = {scenario_labels_js};

const GRID = '#2a2d3a', TICK = '#888';
function baseOpts(yLabel='') {{
  return {{
    animation: false,
    plugins: {{ legend: {{ labels: {{ color:'#ccc' }} }} }},
    scales: {{
      x: {{ ticks:{{color:TICK,maxTicksLimit:20}}, grid:{{color:GRID}} }},
      y: {{ ticks:{{color:TICK}}, grid:{{color:GRID}},
            title:{{display:!!yLabel,text:yLabel,color:TICK}} }}
    }}
  }};
}}

function lineChart(id, datasets, yLabel='') {{
  new Chart(document.getElementById(id), {{
    type:'line', data:{{labels,datasets}}, options:baseOpts(yLabel)
  }});
}}
function barChart(id, datasets, yLabel='') {{
  new Chart(document.getElementById(id), {{
    type:'bar', data:{{labels,datasets}}, options:baseOpts(yLabel)
  }});
}}
function scenBar(id, p50, p95, yLabel='ms') {{
  new Chart(document.getElementById(id), {{
    type:'bar',
    data:{{ labels:scenLabels, datasets:[
      {{label:'p50',data:p50,backgroundColor:'rgba(99,179,237,0.75)',borderColor:'rgb(99,179,237)',borderWidth:1}},
      {{label:'p95',data:p95,backgroundColor:'rgba(248,113,113,0.75)',borderColor:'rgb(248,113,113)',borderWidth:1}},
    ]}},
    options:baseOpts(yLabel)
  }});
}}
function scenSingle(id, data, color, label, yLabel='') {{
  new Chart(document.getElementById(id), {{
    type:'bar',
    data:{{ labels:scenLabels, datasets:[
      {{label,data,backgroundColor:color.replace('rgb','rgba').replace(')',',0.75)'),
        borderColor:color,borderWidth:1}}
    ]}},
    options:baseOpts(yLabel)
  }});
}}

// ── Time series ──────────────────────────────────────────────────────────────
lineChart('latChart', {js(latency_datasets_p95)}, 'ms');

barChart('throughputChart', [
  {{label:'Iterations',data:{js(iter_series)},backgroundColor:'rgba(74,222,128,0.7)',borderColor:'rgb(74,222,128)',borderWidth:1}},
  {{label:'Dropped',data:{js(dropped_series)},backgroundColor:'rgba(248,113,113,0.8)',borderColor:'rgb(248,113,113)',borderWidth:1}},
], 'count');

barChart('errChart', [
  {{label:'RPC Errors',data:{js(errors_series)},backgroundColor:'rgba(248,113,113,0.7)',borderColor:'rgb(248,113,113)',borderWidth:1}}
]);

new Chart(document.getElementById('vuChart'), {{
  type:'line',
  data:{{ labels, datasets:[
    {{label:'VUs',data:{js(vus_series)},borderColor:'rgb(99,179,237)',
      backgroundColor:'rgba(99,179,237,0.1)',fill:true,tension:0.2,pointRadius:0,yAxisID:'y'}},
    {{label:'Success Rate %',data:{js(success_series)},borderColor:'rgb(74,222,128)',
      fill:false,tension:0.3,pointRadius:1,yAxisID:'y2',spanGaps:true}},
  ]}},
  options:{{ animation:false,
    plugins:{{legend:{{labels:{{color:'#ccc'}}}}}},
    scales:{{
      x:{{ticks:{{color:TICK,maxTicksLimit:20}},grid:{{color:GRID}}}},
      y:{{ticks:{{color:TICK}},grid:{{color:GRID}},title:{{display:true,text:'VUs',color:TICK}},position:'left'}},
      y2:{{ticks:{{color:TICK}},grid:{{drawOnChartArea:false}},title:{{display:true,text:'%',color:TICK}},position:'right',min:0,max:100}},
    }}
  }}
}});

lineChart('checkTimeChart', [
  {{label:'Check Pass %',data:{js(check_pass_series)},borderColor:'rgb(250,204,21)',
    fill:false,tension:0.3,pointRadius:1,spanGaps:true}}
], '%');

lineChart('netChart', [
  {{label:'Sent KB/s',data:{js(sent_series)},borderColor:'rgb(99,179,237)',
    backgroundColor:'rgba(99,179,237,0.1)',fill:true,tension:0.3,pointRadius:0}},
  {{label:'Recv KB/s',data:{js(recv_series)},borderColor:'rgb(153,102,255)',
    backgroundColor:'rgba(153,102,255,0.1)',fill:true,tension:0.3,pointRadius:0}},
], 'KB/s');

barChart('streamsChart', [
  {{label:'Streams opened',data:{js(streams_series)},backgroundColor:'rgba(153,102,255,0.7)',
    borderColor:'rgb(153,102,255)',borderWidth:1}}
]);

barChart('integrityTimeChart', [
  {{label:'Integrity Failures',data:{js(integrity_series)},
    backgroundColor:'rgba(248,113,113,0.8)',borderColor:'rgb(248,113,113)',borderWidth:1}}
]);

// ── Per-scenario ─────────────────────────────────────────────────────────────
scenBar('iterDurChart',  {js(iter_dur_p50)},  {js(iter_dur_p95)},  'ms');
scenSingle('droppedScenChart', {js(dropped_scen)},  'rgb(248,113,113)', 'Dropped', 'count');
scenSingle('checkScenChart',   {js(check_pct_scen)},'rgb(250,204,21)',  'Pass %',  '%');
scenBar('cdScenChart',   {js(cd_p50)},   {js(cd_p95)});
scenBar('lsScenChart',   {js(ls_p50)},   {js(ls_p95)});
scenBar('ulScenChart',   {js(ul_p50)},   {js(ul_p95)});
scenBar('dlScenChart',   {js(dl_p50)},   {js(dl_p95)});
scenBar('delScenChart',  {js(del_p50)},  {js(del_p95)});
scenBar('mkdirScenChart',{js(mkdir_p50)},{js(mkdir_p95)});
scenBar('renScenChart',  {js(ren_p50)},  {js(ren_p95)});
scenBar('rendScenChart', {js(rend_p50)}, {js(rend_p95)});
scenBar('treeScenChart', {js(tree_p50)}, {js(tree_p95)});
scenBar('curScenChart',  {js(cur_p50)},  {js(cur_p95)});
</script>
</body>
</html>
"""

OUTPUT.write_text(HTML)
print(f"✓ Written to {OUTPUT}")
