import grpc from 'k6/net/grpc';
import { check, sleep } from 'k6';
import exec from 'k6/execution';

const client = new grpc.Client();
const PROTO_PATH = '../proto/server.proto'; 

client.load(['.'], PROTO_PATH);

export const options = {
  stages: [
    { duration: '10s', target: 3 }, 
    { duration: '30s', target: 3 }, 
    { duration: '10s', target: 0 },  
  ],
};

const seededEmails = [
  'alice@school.edu',
  'bob@school.edu',
  'professor@school.edu'
];

export default () => {
  client.connect('localhost:50051', { plaintext: true });

  const userIndex = (exec.vu.idInTest - 1) % seededEmails.length;
  const userEmail = seededEmails[userIndex]; 
  const params = { metadata: { 'email': userEmail } };

  for (let i = 0; i < 50; i++) {
    client.invoke('main.Server/ChangeDirectory', { folder: '' }, params);

    const pwdRes = client.invoke('main.Server/CurrentDirectory', {}, params);
    check(pwdRes, { 'pwd OK': (r) => r && r.status === grpc.StatusOK });

    const cdColRes = client.invoke('main.Server/ChangeDirectory', { folder: 'Khoury' }, params);
    check(cdColRes, { 'cd Khoury OK': (r) => r && r.status === grpc.StatusOK });

    const cdClassRes = client.invoke('main.Server/ChangeDirectory', { folder: 'CS101' }, params);
    check(cdClassRes, { 'cd CS101 OK': (r) => r && r.status === grpc.StatusOK });

    const lsRes = client.invoke('main.Server/ListDirectory', {}, params);
    check(lsRes, { 'ls OK': (r) => r && r.status === grpc.StatusOK });

    const cdUpRes = client.invoke('main.Server/ChangeDirectory', { folder: '..' }, params);
    check(cdUpRes, { 'cd .. OK': (r) => r && r.status === grpc.StatusOK });

    const resetRes = client.invoke('main.Server/ChangeDirectory', { folder: '' }, params);
    check(resetRes, { 'reset to root OK': (r) => r && r.status === grpc.StatusOK });

    sleep(0.5); 
  }

  client.close();
};