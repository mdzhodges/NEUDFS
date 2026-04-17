import grpc from 'k6/net/grpc';
import { check, sleep, group } from 'k6';
import { Counter, Trend, Rate } from 'k6/metrics';
import exec from 'k6/execution';

const client = new grpc.Client();
client.load(['../proto'], 'server.proto');

const NLB_ADDR = __ENV.NLB_ADDR || 'localhost:50051';
const STUDENTS = JSON.parse(open('./students.json'));
const PROFESSOR = __ENV.PROFESSOR_EMAIL || 'noah.harris@northeastern.edu';

const cdLatency = new Trend('cd_latency', true);
const uploadLatency = new Trend('upload_latency', true);
const downloadLatency = new Trend('download_latency', true);
const lsLatency = new Trend('ls_latency', true);
const treeLatency = new Trend('tree_latency', true);
const deleteLatency = new Trend('delete_latency', true);
const rpcErrors = new Counter('rpc_errors');
const integrityFailures = new Counter('integrity_failures');
const permissionBypass = new Counter('permission_bypass');
const successRate = new Rate('success_rate');

export const options = {
  scenarios: {
    setup_seed: {
      executor: 'per-vu-iterations',
      vus: 1,
      iterations: 1,
      exec: 'seedTestData',
      startTime: '0s',
      tags: { scenario: 'setup' },
    },
    steady_classroom: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 5 },
        { duration: '2m', target: 10 },
        { duration: '1m', target: 10 },
        { duration: '30s', target: 0 },
      ],
      exec: 'studentWorkflow',
      startTime: '10s',
      tags: { scenario: 'steady_classroom' },
    },
    burst_download: {
      executor: 'shared-iterations',
      vus: 10,
      iterations: 10,
      startTime: '4m40s',
      exec: 'burstDownload',
      tags: { scenario: 'burst_download' },
    },
    integrity_check: {
      executor: 'per-vu-iterations',
      vus: 10,
      iterations: 5,
      startTime: '5m40s',
      exec: 'integrityCheck',
      tags: { scenario: 'integrity' },
    },
    upload_delete_cycle: {
      executor: 'per-vu-iterations',
      vus: 10,
      iterations: 3,
      startTime: '6m50s',
      exec: 'uploadDeleteWorkflow',
      tags: { scenario: 'upload_delete_cycle' },
    },
    teacher_race: {
      executor: 'per-vu-iterations',
      vus: 5,
      iterations: 1,
      startTime: '7m10s',
      exec: 'teacherUpdateRace',
      tags: { scenario: 'teacher_race' },
    },
    permission_test: {
      executor: 'shared-iterations',
      vus: 10,
      iterations: 10,
      startTime: '8m10s',
      exec: 'permissionBoundary',
      tags: { scenario: 'permission_test' },
    },
    large_file: {
      executor: 'per-vu-iterations',
      vus: 5,
      iterations: 1,
      startTime: '9m10s',
      exec: 'largeFileTest',
      tags: { scenario: 'large_file' },
    },
    rapid_fire: {
      executor: 'constant-arrival-rate',
      rate: 50,
      timeUnit: '1s',
      duration: '1m',
      preAllocatedVUs: 10,
      startTime: '10m40s',
      exec: 'rapidNavigation',
      tags: { scenario: 'rapid_fire' },
    },
    spike_test: {
      executor: 'ramping-vus',
      startVUs: 2,
      stages: [
        { duration: '30s', target: 2 },
        { duration: '10s', target: 10 },
        { duration: '2m', target: 10 },
        { duration: '10s', target: 2 },
        { duration: '1m', target: 2 },
      ],
      startTime: '12m10s',
      exec: 'scaleWorkflow',
      tags: { scenario: 'spike_test' },
    },
    soak_test: {
      executor: 'constant-vus',
      vus: 10,
      duration: '10m',
      startTime: '16m10s',
      exec: 'soakWorkflow',
      tags: { scenario: 'soak_test' },
    },
    tree_stress: {
      executor: 'shared-iterations',
      vus: 10,
      iterations: 20,
      startTime: '27m10s',
      exec: 'treeStress',
      tags: { scenario: 'tree_stress' },
    },
    session_conflict_upload: {
      executor: 'per-vu-iterations',
      vus: 5,
      iterations: 3,
      startTime: '29m',
      exec: 'sessionConflictUpload',
      tags: { scenario: 'session_conflict_upload' },
    },
    session_conflict_navigate: {
      executor: 'per-vu-iterations',
      vus: 5,
      iterations: 10,
      startTime: '29m',
      exec: 'sessionConflictNavigate',
      tags: { scenario: 'session_conflict_navigate' },
    },
    mkdirWorkflow: {
        executor: 'per-vu-iterations',
        vus:1,
        iterations:10,
        startTime: '30m',
        exec: 'mkdirWorkflow',
        tags: {scenario:'mkdirWorkflow'}
    },
    renameDirWorkflow: {
        executor: 'per-vu-iterations',
        vus: 1,
        iterations: 1,
        startTime: '31m',
        exec: 'renameDirWorkflow',
        tags: { scenario: 'renameDirWorkflow' },
    },
    renameFileWorkflow: {
        executor: 'per-vu-iterations',
        vus: 1,
        iterations: 1,
        startTime: '32m',
        exec: 'renameFileWorkflow',
        tags: { scenario: 'renameFileWorkflow' },
    },
    concurrentRenameStress: {
        executor: 'per-vu-iterations',
        vus: 5,
        iterations: 10,
        startTime: '33m',
        exec: 'concurrentRenameStress',
        tags: { scenario: 'concurrentRenameStress' },
    },
    concurrentCdRaceStress: {
        executor: 'per-vu-iterations',
        vus: 5,
        iterations: 10,
        startTime: '33m',
        exec: 'concurrentCdRaceStress',
        tags: { scenario: 'concurrentCdRaceStress' },
    },
  },

  thresholds: {
    'cd_latency': ['p(95)<200', 'p(99)<500'],
    'upload_latency': ['p(95)<2000'],
    'download_latency': ['p(95)<1000'],
    'ls_latency': ['p(95)<300'],
    'tree_latency': ['p(95)<3000'],
    'rpc_errors': ['count<500'],
    'integrity_failures': ['count==0'],
    'permission_bypass': ['count==0'],
    'success_rate': ['rate>0.70'],
  },
};

function connect() {
  client.connect(NLB_ADDR, { plaintext: true, timeout: '10s' });
}

function meta(email) {
  return { metadata: { email: email } };
}

function timedInvoke(metric, method, payload, email) {
  const start = Date.now();
  const resp = client.invoke(method, payload, meta(email));
  metric.add(Date.now() - start);
  const ok = resp && resp.status === grpc.StatusOK;
  successRate.add(ok ? 1 : 0);
  if (!ok) rpcErrors.add(1);
  return resp;
}

function navigate(email, folders) {
  for (let attempt = 0; attempt < 5; attempt++) {
    client.invoke('main.Server/ChangeDirectory', { folder: '' }, meta(email));
    let success = true;
    for (const folder of folders) {
      const resp = client.invoke('main.Server/ChangeDirectory', { folder: folder }, meta(email));
      if (!resp || resp.status !== grpc.StatusOK) {
        success = false;
        // Jittered backoff: spreads retries so competing VUs on the same account don't collide again
        sleep(0.1 * (attempt + 1) + Math.random() * 0.1);
        break;
      }
    }
    if (success) return true;
  }
  return false;
}

function uploadBytes(email, filename, data) {
  const start = Date.now();
  const stream = new grpc.Stream(client, 'main.Server/Upload', meta(email));

  let success = false;
  let errored = false;

  stream.on('data', (_resp) => {
    success = true;
  });
  stream.on('error', (_err) => {
    rpcErrors.add(1);
    errored = true;
  });

  stream.write({ metadata: { name: filename, content_type: 'text/plain' } });

  const chunkSize = 65536;
  for (let i = 0; i < data.length; i += chunkSize) {
    const end = Math.min(i + chunkSize, data.length);
    stream.write({ chunk: data.slice(i, end) });
  }

  stream.end();

  // Allow the server time to process and respond.
  // Timeout scales with file size: 1ms per KB, minimum 5s.
  const waitMs = Math.max(5000, data.length / 1024);
  sleep(waitMs / 1000);

  uploadLatency.add(Date.now() - start);
  return success && !errored;
}

// Each scenario gets a different starting offset so VU 1 in two concurrent
// scenarios doesn't hash to the same student and stack uploads on one user.
// Stride of 7 (prime) avoids alignment even for small STUDENTS arrays.
const SCENARIO_STUDENT_OFFSET = {
  steady_classroom:           0,
  burst_download:             7,
  integrity_check:            14,
  upload_delete_cycle:        21,
  large_file:                 28,
  rapid_fire:                 35,
  spike_test:                 42,
  soak_test:                  49,
  tree_stress:                56,
  session_conflict_upload:    63,
  session_conflict_navigate:  70,
};

function getStudentEmail() {
  const offset = SCENARIO_STUDENT_OFFSET[exec.scenario.name] || 0;
  return STUDENTS[(__VU - 1 + offset) % STUDENTS.length];
}

function getStudentFolder(email) {
  return email.split('@')[0].replace(/\./g, '_');
}

export function seedTestData() {
  connect();

  const data = new Uint8Array(10240);
  for (let i = 0; i < data.length; i++) data[i] = i % 256;

  // --- existing seed ---
  navigate(PROFESSOR, ['Khoury', 'CS5010', 'announcements']);
  uploadBytes(PROFESSOR, 'lecture.txt', data);

  navigate(PROFESSOR, ['Khoury', 'CS5010']);
  uploadBytes(PROFESSOR, 'proff_rename_test.txt', data);

  // --- renameDirWorkflow seed ---
  // Professor-owned shared folders at class level (to be renamed by prof, blocked for students)
  navigate(PROFESSOR, ['Khoury', 'CS5010']);
  for (let i = 0; i < 3; i++) {
    client.invoke('main.Server/MakeDirectory', { name: `rename_dir_shared_${i}` }, meta(PROFESSOR));
  }

  // Student subfolders in personal dirs (to be renamed by the owning student)
  for (let i = 0; i < STUDENTS.length; i++) {
    const email = STUDENTS[i];
    const folder = getStudentFolder(email);
    if (!navigate(email, ['Khoury', 'CS5010', folder])) continue;
    client.invoke('main.Server/MakeDirectory', { name: 'rename_dir_personal' }, meta(email));
  }

  // --- renameFileWorkflow seed ---
  // Files in shared announcements folder (prof can rename, students cannot)
  navigate(PROFESSOR, ['Khoury', 'CS5010', 'announcements']);
  uploadBytes(PROFESSOR, 'rename_file_shared.txt', data);

  // Files at class level (prof can rename, students cannot)
  navigate(PROFESSOR, ['Khoury', 'CS5010']);
  uploadBytes(PROFESSOR, 'rename_file_class.txt', data);

  // Professor-uploaded files in each student's folder (prof can rename)
  for (let i = 0; i < STUDENTS.length; i++) {
    const email = STUDENTS[i];
    const folder = getStudentFolder(email);
    if (!navigate(PROFESSOR, ['Khoury', 'CS5010', folder])) continue;
    uploadBytes(PROFESSOR, 'rename_file_by_prof.txt', data);
  }

  // Student-owned files in personal folder and in the rename_dir_personal subfolder
  for (let i = 0; i < STUDENTS.length; i++) {
    const email = STUDENTS[i];
    const folder = getStudentFolder(email);
    if (!navigate(email, ['Khoury', 'CS5010', folder])) continue;
    uploadBytes(email, 'rename_file_personal.txt', data);
    if (navigate(email, ['Khoury', 'CS5010', folder, 'notes'])) {
      uploadBytes(email, 'rename_file_in_subfolder.txt', data);
    }
  }

  // --- concurrentRenameStress seed ---
  // Each student gets 3 subdirs and 3 files in their personal folder to race against
  const smallData = new Uint8Array(512);
  for (let i = 0; i < STUDENTS.length; i++) {
    const email = STUDENTS[i];
    const folder = getStudentFolder(email);
    if (!navigate(email, ['Khoury', 'CS5010', folder])) continue;
    for (let j = 0; j < 3; j++) {
      client.invoke('main.Server/MakeDirectory', { name: `stress_subdir_${j}` }, meta(email));
      uploadBytes(email, `stress_file_${j}.txt`, smallData);
    }
  }

  console.log('Seed complete: lecture, rename dir/file fixtures uploaded for all workflows');
  client.close();
}

export function studentWorkflow() {
  connect();
  const email = getStudentEmail();
  const folder = getStudentFolder(email);

  group('navigate', () => {
    navigate(email, ['Khoury', 'CS5010', folder]);
  });
    group('current directory', () => {                                                                                                               
        client.invoke('main.Server/CurrentDirectory', {}, meta(email));
    });   
  group('list', () => {
    timedInvoke(lsLatency, 'main.Server/ListDirectory', {}, email);
  });

  group('upload', () => {
    const data = new Uint8Array(1024);
    uploadBytes(email, `hw_${__VU}_${__ITER}.txt`, data);
  });

  group('download', () => {
    timedInvoke(downloadLatency, 'main.Server/Download',
      { name: `hw_${__VU}_${__ITER}.txt` }, email);
  });

  group('cleanup', () => {
    client.invoke('main.Server/Delete',
      { path: `hw_${__VU}_${__ITER}.txt` }, meta(email));
  });

  sleep(Math.random() * 2 + 1);
  client.close();
}

export function burstDownload() {
  connect();
  const email = getStudentEmail();
  navigate(email, ['Khoury', 'CS5010', 'announcements']);

  timedInvoke(downloadLatency, 'main.Server/Download',
    { name: 'lecture.txt' }, email);

  client.close();
}

export function integrityCheck() {
  connect();
  const email = getStudentEmail();
  const folder = getStudentFolder(email);
  navigate(email, ['Khoury', 'CS5010', folder]);

  const sizes = [1024, 10240, 102400, 1048576];
  const size = sizes[__ITER % sizes.length];
  const filename = `integrity_${__VU}_${__ITER}_${size}.bin`;

  const original = new Uint8Array(size);
  for (let i = 0; i < size; i++) {
    original[i] = ((__VU * 7 + __ITER * 13 + i * 3) % 256);
  }

  group('upload', () => {
    const ok = uploadBytes(email, filename, original);
    check(ok, { 'integrity upload ok': (v) => v === true });
  });

  group('download and verify', () => {
    const start = Date.now();
    const resp = client.invoke('main.Server/Download', { name: filename }, meta(email));
    downloadLatency.add(Date.now() - start);

    if (!resp || resp.status !== grpc.StatusOK) {
      integrityFailures.add(1);
      return;
    }

    const downloaded = resp.message.data;
    if (!downloaded || downloaded.length !== original.length) {
      integrityFailures.add(1);
      console.error(`Size mismatch: expected ${original.length}, got ${downloaded ? downloaded.length : 0}`);
      return;
    }

    for (let i = 0; i < original.length; i++) {
      if (downloaded[i] !== original[i]) {
        integrityFailures.add(1);
        console.error(`Byte mismatch at offset ${i}: expected ${original[i]}, got ${downloaded[i]}`);
        return;
      }
    }
  });

  client.invoke('main.Server/Delete', { path: filename }, meta(email));
  client.close();
}

export function uploadDeleteWorkflow() {
  connect();
  const email = getStudentEmail();
  const folder = getStudentFolder(email);
  navigate(email, ['Khoury', 'CS5010', folder]);

  const data = new Uint8Array(4096);
  for (let i = 0; i < data.length; i++) {
    data[i] = (i * 17) % 256;
  }
  const filename = `cycle_${__VU}_${__ITER}.bin`;
  let failed = false;

  group('upload', () => {
    const ok = uploadBytes(email, filename, data);
    check(ok, { 'cycle upload ok': (v) => v === true });
    if (!ok) {
      failed = true;
    }
  });
  if (failed) {
    client.close();
    return;
  }

  group('download', () => {
    const start = Date.now();
    const resp = client.invoke('main.Server/Download', { name: filename }, meta(email));
    downloadLatency.add(Date.now() - start);
    check(resp, { 'cycle download ok': (r) => r && r.status === grpc.StatusOK });
    if (!resp || resp.status !== grpc.StatusOK) {
      rpcErrors.add(1);
      failed = true;
      return;
    }

    const downloaded = resp.message.data;
    const sameLength = downloaded && downloaded.length === data.length;
    check(downloaded, { 'cycle download has data': (v) => v && v.length === data.length });
    if (!sameLength) {
      rpcErrors.add(1);
      console.error(`cycle size mismatch for ${filename}`);
      failed = true;
      return;
    }
    for (let i = 0; i < data.length; i++) {
      if (downloaded[i] !== data[i]) {
        rpcErrors.add(1);
        console.error(`cycle byte mismatch at ${i} for ${filename}`);
        failed = true;
        return;
      }
    }
  });
  if (failed) {
    client.close();
    return;
  }

  group('delete', () => {
    const start = Date.now();
    const resp = client.invoke('main.Server/Delete', { path: filename }, meta(email));
    deleteLatency.add(Date.now() - start);
    check(resp, { 'cycle delete ok': (r) => r && r.status === grpc.StatusOK });
    if (!resp || resp.status !== grpc.StatusOK) {
      rpcErrors.add(1);
      failed = true;
    }
  });
  if (failed) {
    client.close();
    return;
  }

  group('post-delete download', () => {
    const resp = client.invoke('main.Server/Download', { name: filename }, meta(email));
    const blocked = !resp || resp.status !== grpc.StatusOK;
    check(blocked, { 'post-delete download not found': (v) => v === true });
    if (!blocked) {
      rpcErrors.add(1);
      console.error(`SECURITY: ${email} downloaded ${filename} after delete`);
    }
  });

  client.close();
}

export function teacherUpdateRace() {
  connect();
  navigate(PROFESSOR, ['Khoury', 'CS5010', 'announcements']);

  // Each VU uploads a distinct version of the same file concurrently.
  // With vus:5, all five uploads race against each other on S3 + DynamoDB.
  // One write wins; the others overwrite it — last writer wins on S3.
  const vData = new Uint8Array(10240);
  vData.fill(__VU); // distinct byte pattern per VU so we can tell who won
  const uploadOk = uploadBytes(PROFESSOR, 'race_file.txt', vData);
  check(uploadOk, { 'concurrent upload completed': (v) => v === true });

  // After all VUs finish uploading, verify the file is in a readable, complete state.
  // Any version is valid — what must NOT happen is a partial or corrupt result.
  const resp = client.invoke('main.Server/Download',
    { name: 'race_file.txt' }, meta(PROFESSOR));
  check(resp, { 'race file readable after concurrent uploads': (r) => r && r.status === grpc.StatusOK });
  if (resp && resp.status === grpc.StatusOK) {
    const data = resp.message && resp.message.data;
    check(data, { 'race file has full content': (d) => d && d.length === 10240 });
    if (data && data.length === 10240) {
      // All bytes should be the same value (one VU's fill won cleanly, not a mix)
      const firstByte = data[0];
      let corrupt = false;
      for (let i = 1; i < data.length; i++) {
        if (data[i] !== firstByte) { corrupt = true; break; }
      }
      check(!corrupt, { 'race file not corrupted (single winner)': (v) => v === true });
      if (corrupt) integrityFailures.add(1);
    }
  }

  // Only VU 1 deletes to avoid a delete race on top of the upload race.
  if (__VU === 1) {
    const start = Date.now();
    const delResp = client.invoke('main.Server/Delete',
      { path: 'race_file.txt' }, meta(PROFESSOR));
    deleteLatency.add(Date.now() - start);
    check(delResp, { 'race delete ok': (r) => r && r.status === grpc.StatusOK });
  }

  client.close();
}

export function permissionBoundary() {
  connect();
  const attackerIdx = (__VU - 1) % STUDENTS.length;
  const victimIdx = (attackerIdx + 1) % STUDENTS.length;
  const attacker = STUDENTS[attackerIdx];
  const victim = STUDENTS[victimIdx];
  const victimFolder = getStudentFolder(victim);

  navigate(attacker, ['Khoury', 'CS5010']);

  group('cd into other student folder', () => {
    const resp = client.invoke('main.Server/ChangeDirectory',
      { folder: victimFolder }, meta(attacker));
    const blocked = !resp || resp.status !== grpc.StatusOK;
    check(blocked, { 'cd blocked': (v) => v === true });
    if (!blocked) {
      permissionBypass.add(1);
      console.error(`SECURITY: ${attacker} accessed ${victimFolder}`);
    }
  });

  group('upload to class root as student', () => {
    navigate(attacker, ['Khoury', 'CS5010']);

    const stream = new grpc.Stream(client, 'main.Server/Upload', meta(attacker));
    let blocked = true;
    stream.on('data', () => { blocked = false; });
    stream.on('error', () => {});

    stream.write({ metadata: { name: 'malicious.txt', content_type: 'text/plain' } });
    stream.write({ chunk: new Uint8Array([65, 66, 67]) });
    stream.end();

    check(blocked, { 'root upload blocked': (v) => v === true });
    if (!blocked) {
      permissionBypass.add(1);
      console.error(`SECURITY: ${attacker} uploaded to class root`);
    }
  });

  group('mkdir in shared folder as student', () => {
    navigate(attacker, ['Khoury', 'CS5010', 'announcements']);
    const resp = client.invoke('main.Server/MakeDirectory',
      { name: 'hacked' }, meta(attacker));
    const blocked = !resp || resp.status !== grpc.StatusOK;
    check(blocked, { 'mkdir blocked': (v) => v === true });
    if (!blocked) {
      permissionBypass.add(1);
      console.error(`SECURITY: ${attacker} created folder in shared dir`);
    }
  });

  group('delete file in shared folder as student', () => {
    navigate(attacker, ['Khoury', 'CS5010', 'announcements']);
    const resp = client.invoke('main.Server/Delete',
      { path: 'lecture.txt' }, meta(attacker));
    const blocked = !resp || resp.status !== grpc.StatusOK;
    check(blocked, { 'shared delete blocked': (v) => v === true });
    if (!blocked) {
      permissionBypass.add(1);
      console.error(`SECURITY: ${attacker} deleted file in shared folder`);
    }
  });

  group('delete file at class root as student', () => {
    navigate(attacker, ['Khoury', 'CS5010']);
    const resp = client.invoke('main.Server/Delete',
      { path: 'proff_rename_test.txt' }, meta(attacker));
    const blocked = !resp || resp.status !== grpc.StatusOK;
    check(blocked, { 'class root delete blocked': (v) => v === true });
    if (!blocked) {
      permissionBypass.add(1);
      console.error(`SECURITY: ${attacker} deleted file at class root`);
    }
  });

  group('upload to shared folder as student', () => {
    navigate(attacker, ['Khoury', 'CS5010', 'announcements']);

    const stream = new grpc.Stream(client, 'main.Server/Upload', meta(attacker));
    let blocked = true;
    stream.on('data', () => { blocked = false; });
    stream.on('error', () => {});

    stream.write({ metadata: { name: 'malicious_shared.txt', content_type: 'text/plain' } });
    stream.write({ chunk: new Uint8Array([65, 66, 67]) });
    stream.end();

    check(blocked, { 'shared upload blocked': (v) => v === true });
    if (!blocked) {
      permissionBypass.add(1);
      console.error(`SECURITY: ${attacker} uploaded to shared folder`);
    }
  });

  client.close();
}

export function largeFileTest() {
  connect();
  const email = getStudentEmail();
  const folder = getStudentFolder(email);
  navigate(email, ['Khoury', 'CS5010', folder]);

  const sizes = [
    { name: '1mb', bytes: 1048576 },
    { name: '5mb', bytes: 5242880 },
    { name: '10mb', bytes: 10485760 },
    { name: '25mb', bytes: 26214400 },
    { name: '50mb', bytes: 52428800 },
  ];
  const size = sizes[__VU % sizes.length];
  const filename = `large_${size.name}_vu${__VU}.bin`;

  console.log(`VU ${__VU}: uploading ${size.name} file`);

  group(`upload ${size.name}`, () => {
    const data = new Uint8Array(size.bytes);
    for (let i = 0; i < data.length; i++) data[i] = i % 256;
    const ok = uploadBytes(email, filename, data);
    check(ok, { [`${size.name} upload ok`]: (v) => v === true });
  });

  group(`download ${size.name}`, () => {
    const start = Date.now();
    const resp = client.invoke('main.Server/Download',
      { name: filename }, meta(email));
    const elapsed = Date.now() - start;
    downloadLatency.add(elapsed);
    console.log(`VU ${__VU}: downloaded ${size.name} in ${elapsed}ms`);
    check(resp, { [`${size.name} download ok`]: (r) => r && r.status === grpc.StatusOK });
  });

  client.invoke('main.Server/Delete', { path: filename }, meta(email));
  client.close();
}

export function rapidNavigation() {
  connect();
  const email = getStudentEmail();

  const start = Date.now();
  navigate(email, ['Khoury', 'CS5010']);
  cdLatency.add(Date.now() - start);

  timedInvoke(lsLatency, 'main.Server/ListDirectory', {}, email);

  client.close();
}

export function scaleWorkflow() {
  connect();
  const email = getStudentEmail();

  group('navigate', () => {
    const start = Date.now();
    navigate(email, ['Khoury', 'CS5010']);
    cdLatency.add(Date.now() - start);
  });

  group('list', () => {
    timedInvoke(lsLatency, 'main.Server/ListDirectory', {}, email);
  });

  group('download', () => {
    navigate(email, ['Khoury', 'CS5010', 'announcements']);
    timedInvoke(downloadLatency, 'main.Server/Download',
      { name: 'lecture.txt' }, email);
  });

  sleep(Math.random() + 0.5);
  client.close();
}

export function soakWorkflow() {
  connect();
  const email = getStudentEmail();
  const folder = getStudentFolder(email);

  navigate(email, ['Khoury', 'CS5010', folder]);
  timedInvoke(lsLatency, 'main.Server/ListDirectory', {}, email);

  const data = new Uint8Array(2048);
  const filename = `soak_${__VU}_${__ITER}.txt`;
  uploadBytes(email, filename, data);

  timedInvoke(downloadLatency, 'main.Server/Download',
    { name: filename }, email);

  const start = Date.now();
  client.invoke('main.Server/Delete', { path: filename }, meta(email));
  deleteLatency.add(Date.now() - start);

  navigate(email, ['Khoury', 'CS5010', 'announcements']);
  timedInvoke(lsLatency, 'main.Server/ListDirectory', {}, email);

  sleep(Math.random() * 3 + 1);
  client.close();
}

export function treeStress() {
  connect();
  const email = getStudentEmail();
  const folder = getStudentFolder(email);

  group('tree from root', () => {
    navigate(email, []);
    timedInvoke(treeLatency, 'main.Server/TreeDirectory', {}, email);
  });

  group('tree from college', () => {
    navigate(email, ['Khoury']);
    timedInvoke(treeLatency, 'main.Server/TreeDirectory', {}, email);
  });

  group('tree from class', () => {
    navigate(email, ['Khoury', 'CS5010']);
    timedInvoke(treeLatency, 'main.Server/TreeDirectory', {}, email);
  });

  // Clean up any hw_* files left behind by studentWorkflow runs for this VU.
  // Navigate to the student's folder and delete files for all iterations seen.
  group('cleanup stale hw files', () => {
    if (!navigate(email, ['Khoury', 'CS5010', folder])) return;
    for (let iter = 0; iter < 200; iter++) {
      const resp = client.invoke('main.Server/Delete',
        { path: `hw_${__VU}_${iter}.txt` }, meta(email));
      // Stop once we hit a run of NotFound — no more files for this VU
      if (!resp || resp.status === grpc.StatusNotFound) break;
    }
  });

  client.close();
}

export function sessionConflictUpload() {
  connect();
  const email = getStudentEmail();
  const folder = getStudentFolder(email);
  navigate(email, ['Khoury', 'CS5010', folder]);

  const data = new Uint8Array(1024);
  uploadBytes(email, `conflict_${__VU}_${__ITER}.txt`, data);

  sleep(0.5);
  client.close();
}

export function sessionConflictNavigate() {
  connect();
  const email = getStudentEmail();
  const folder = getStudentFolder(email);

  navigate(email, ['Khoury', 'CS5010', folder]);
  timedInvoke(lsLatency, 'main.Server/ListDirectory', {}, email);
  navigate(email, ['Khoury', 'CS5010']);
  timedInvoke(lsLatency, 'main.Server/ListDirectory', {}, email);
  navigate(email, ['Khoury', 'CS5010', folder]);

  sleep(0.2);
  client.close();
}

export function mkdirWorkflow() {
    connect()
    group("teacher mkdir in shared folder spaces",()=>{
        navigate(PROFESSOR,['Khoury','CS5010']);
        for(let i=0;i< 5;i++){
            const resp = client.invoke('main.Server/MakeDirectory',
        { name: `shared_test_${i}` }, meta(PROFESSOR));
        check(resp, { 'teacher mkdir ok': (r) => r && r.status === grpc.StatusOK });
        }
    })
    group("students creating folders in their personal folder",()=>{
        for(let i=0;i<STUDENTS.length;i++){
            const email = STUDENTS[i]
            const folder = getStudentFolder(email);
            if (!navigate(email, ['Khoury', 'CS5010', folder])) continue;
            const resp = client.invoke('main.Server/MakeDirectory',{name:`personal_test_${i}`},meta(email));
            check(resp,{'student mkdir ok':(r) => r && r.status === grpc.StatusOK});
        }
    })
    group("students should not be able to create folders at class level",()=>{
        for(let i=0;i<STUDENTS.length;i++){
            const email = STUDENTS[i]
            navigate(email,['Khoury','CS5010']);
            const resp = client.invoke('main.Server/MakeDirectory',{name:`unauthorized_folder`},meta(email));
            const blocked = !resp || resp.status !== grpc.StatusOK;
            check(blocked,{'student mkdir is blocked!':(v) => v === true})
            if (!blocked) {
                permissionBypass.add(1);
                console.error('SECURITY: student created folder at class level');
            }
        }
    })
        group("students should not be able to create folders at shared level",()=>{
        for(let i=0;i<STUDENTS.length;i++){
            const email = STUDENTS[i];
            navigate(email,['Khoury','CS5010','announcements']);
            const resp = client.invoke('main.Server/MakeDirectory',{name:`unauthorized_folder`},meta(email));
            const blocked = !resp || resp.status !== grpc.StatusOK;
            check(blocked,{'student mkdir is blocked!':(v) => v === true})
            if (!blocked) {
                permissionBypass.add(1);
                console.error('SECURITY: student created folder at class level');
            }
        }
    })
    group('cleanup professor shared folders',()=>{
        navigate(PROFESSOR,['Khoury','CS5010'])
        for(let i=0;i<5;i++){
            client.invoke('main.Server/Delete',{path:`shared_test_${i}`},meta(PROFESSOR));
        }
    })
    group('cleanup student subfolders',()=>{
        for(let i=0;i<STUDENTS.length;i++){
            const email = STUDENTS[i]
            const folder = getStudentFolder(email)
            navigate(PROFESSOR, ['Khoury', 'CS5010', folder])
            client.invoke('main.Server/Delete',{path:`personal_test_${i}`},meta(PROFESSOR));
        }
    })
    client.close();
}
//renameDir is entry and name,todo
export function renameDirWorkflow(){
    connect()
    group('professors can rename their shared folders',()=>{
        navigate(PROFESSOR,['Khoury','CS5010'])
        for (let i = 0; i < 3; i++) {
            const resp= client.invoke('main.Server/RenameDirectory',{entry: `rename_dir_shared_${i}`, name:`new_name_dir_shared_${i}`},meta(PROFESSOR))
            check(resp,{'teach rename dir ok':(r) => r && r.status === grpc.StatusOK});
        }
    })
    group('students can rename subfolders in their personal folders',()=>{
        for (let i = 0; i < STUDENTS.length; i++) {
            const email = STUDENTS[i];
            const folder = getStudentFolder(email);
            if (!navigate(email, ['Khoury', 'CS5010', folder])) continue;
            const resp = client.invoke('main.Server/RenameDirectory',{entry:`rename_dir_personal`,name:`new_rename_dir_personal_${i}`},meta(email))
            check(resp,{'student renamed dir ok':(r) => r && r.status === grpc.StatusOK})
        }
    })
    group('students cannot rename folders that are shared in class directory',()=>{
        for(let i=0;i < STUDENTS.length;i++) {
            const email = STUDENTS[i]
            navigate(email,['Khoury','CS5010'])
            const resp = client.invoke('main.Server/RenameDirectory',{entry:'announcements',name:'unauthorized_user_renamedir'},meta(email))
            const blocked = !resp || resp.status !== grpc.StatusOK;
            check(blocked,{'student renamedir is blocked!':(v) => v === true})
            if (!blocked) {
                permissionBypass.add(1);
                console.error('SECURITY: student renamed folder at class level');
            }
        }
    })
    group('cleanup renamed shared folders',()=>{
        navigate(PROFESSOR,['Khoury','CS5010'])
        for (let i = 0; i < 3; i++) {
            client.invoke('main.Server/Delete',{path:`new_name_dir_shared_${i}`},meta(PROFESSOR));
        }
    })
    group('cleanup renamed student personal folders',()=>{
        for (let i = 0; i < STUDENTS.length; i++) {
            const email = STUDENTS[i];
            const folder = getStudentFolder(email);
            navigate(PROFESSOR, ['Khoury', 'CS5010', folder]);
            client.invoke('main.Server/Delete',{path:`new_rename_dir_personal_${i}`},meta(PROFESSOR));
        }
    })
    client.close()
}
//renameFile is also entry and name, todo
export function renameFileWorkflow(){
    connect()
    group('professors can rename their files anywhere in class',()=>{
        navigate(PROFESSOR, ['Khoury', 'CS5010']);
        const resp = client.invoke('main.Server/Rename',{entry:'rename_file_class.txt',name:'file_class.txt'},meta(PROFESSOR))
        check(resp,{'teacher can rename a file in class dir ok':(r) => r && r.status === grpc.StatusOK})
    })
    group('professors can rename their files anywhere in a shared folder',()=>{
        navigate(PROFESSOR, ['Khoury', 'CS5010','announcements']);
        const resp = client.invoke('main.Server/Rename',{entry:'rename_file_shared.txt',name:'file_shared.txt'},meta(PROFESSOR))
        check(resp,{'teacher can rename a file in shared sub dir ok':(r) => r && r.status === grpc.StatusOK})
    })
    group('professors can rename files in student folders',()=>{
        for(let i=0;i<STUDENTS.length;i++){
            const email = STUDENTS[i]
            const folder = getStudentFolder(email)
            if (!navigate(PROFESSOR,['Khoury','CS5010',folder])) continue
            const resp = client.invoke('main.Server/Rename',{entry:'rename_file_by_prof.txt',name:'file_by_prof.txt'},meta(PROFESSOR))
            check(resp,{'teacher can rename a file in a student personal folder':(r)=> r && r.status === grpc.StatusOK})
        }
    })
    group('students can rename files in their personal folders',()=>{
        for(let i=0;i< STUDENTS.length;i++){
            const email = STUDENTS[i]
            const folder = getStudentFolder(email)
            if (!navigate(email,['Khoury','CS5010',folder])) continue
            const resp = client.invoke('main.Server/Rename',{entry:'rename_file_personal.txt',name:'file_personal.txt'},meta(email))
            check(resp,{'student can rename a file in their personal folder':(r)=> r && r.status === grpc.StatusOK})
        }
    })
    group('students can rename files in sub folders of their personal folder',()=>{
        for(let i=0;i<STUDENTS.length;i++){
            const email = STUDENTS[i]
            const folder = getStudentFolder(email)
            if (!navigate(email, ['Khoury', 'CS5010', folder, 'notes'])) continue
            const resp = client.invoke('main.Server/Rename',{entry:'rename_file_in_subfolder.txt',name:'file_in_subfolder.txt'},meta(email))
            check(resp,{'student can rename a file in their personal sub folder':(r)=> r && r.status === grpc.StatusOK})
        }
    })
    group('students cannot rename files in class directory',()=>{
        for(let i=0;i<STUDENTS.length;i++){
            const email = STUDENTS[i]
            if (!navigate(email,['Khoury','CS5010']))continue
            const resp = client.invoke('main.Server/Rename',{entry:'file_class.txt',name:'unauth_file_class.txt'},meta(email))
            const blocked = !resp || resp.status !== grpc.StatusOK;
            check(blocked,{'student rename file is blocked!':(v) => v === true})
            if (!blocked) {
                permissionBypass.add(1);
                console.error('SECURITY: student renamed file at class level');
            }
        }
    })
    group('students cannot rename files in shared folders',()=>{
        for(let i=0;i<STUDENTS.length;i++){
            const email = STUDENTS[i]
            if (!navigate(email,['Khoury','CS5010','announcements']))continue
            const resp = client.invoke('main.Server/Rename',{entry:'file_shared.txt',name:'unauth_file_shared.txt'},meta(email))
            const blocked = !resp || resp.status !== grpc.StatusOK;
            check(blocked,{'student rename file is blocked!':(v) => v === true})
            if (!blocked) {
                permissionBypass.add(1);
                console.error('SECURITY: student renamed file at shared folder level');
            }
        }
    })
    client.close();
}

export function concurrentRenameStress() {
    connect();
    const email = getStudentEmail();
    const folder = getStudentFolder(email);
    const idx = __ITER % 3;

    group('concurrent file rename', () => {
        if (!navigate(email, ['Khoury', 'CS5010', folder])) return;
        const from = `stress_file_${idx}.txt`;
        const to = `stress_file_${idx}_renamed.txt`;
        const resp = client.invoke('main.Server/Rename', { entry: from, name: to }, meta(email));
        check(resp, { 'concurrent file rename ok': (r) => r && r.status === grpc.StatusOK });
        if (resp && resp.status === grpc.StatusOK) {
            navigate(email, ['Khoury', 'CS5010', folder]);
            client.invoke('main.Server/Rename', { entry: to, name: from }, meta(email));
        }
    });

    group('concurrent dir rename', () => {
        if (!navigate(email, ['Khoury', 'CS5010', folder])) return;
        const from = `stress_subdir_${idx}`;
        const to = `stress_subdir_${idx}_renamed`;
        const resp = client.invoke('main.Server/RenameDirectory', { entry: from, name: to }, meta(email));
        check(resp, { 'concurrent dir rename ok': (r) => r && r.status === grpc.StatusOK });
        if (resp && resp.status === grpc.StatusOK) {
            navigate(email, ['Khoury', 'CS5010', folder]);
            client.invoke('main.Server/RenameDirectory', { entry: to, name: from }, meta(email));
        }
    });

    client.close();
}

export function concurrentCdRaceStress() {
    connect();
    const email = getStudentEmail();
    const folder = getStudentFolder(email);
    const idx = __ITER % 3;

    group('cd into dir being concurrently renamed', () => {
        if (!navigate(email, ['Khoury', 'CS5010', folder])) return;

        // try the original name — may succeed or fail depending on rename timing
        const oldResp = client.invoke('main.Server/ChangeDirectory',
            { folder: `stress_subdir_${idx}` }, meta(email));
        const oldOk = oldResp && oldResp.status === grpc.StatusOK;

        // navigate back and try the renamed version
        navigate(email, ['Khoury', 'CS5010', folder]);
        const newResp = client.invoke('main.Server/ChangeDirectory',
            { folder: `stress_subdir_${idx}_renamed` }, meta(email));
        const newOk = newResp && newResp.status === grpc.StatusOK;

        // both failing means dir is in neither state — flag it
        if (!oldOk && !newOk) {
            rpcErrors.add(1);
            console.warn(`cd race: stress_subdir_${idx} found in neither state for ${email}`);
        }
    });

    client.close();
}
