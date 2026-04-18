# NEUDFS Final Project Experiments Design & Report

**Team:** Charles, Matt, Jordan, Gyula
**Date:** April 17, 2026
**Course:** CS6650

---

## Introduction

NEUDFS (Northeastern University Distributed File System) is a high-performance storage solution designed to handle the heavy demands of an academic environment. The system is meant to combine the simplicity of a local file system with the massive scale of cloud storage. This allows students and instructors to manage course materials and assignments through a familiar, hierarchical folder structure. As safety is paramount to our design, we built security directly into the architecture using Role-Based Access Control. This keeps student work private while giving professors and TAs the necessary administrative tools to manage shared resources.

NEUDFS relies on a modern distributed stack optimized for high availability and low latency. Built with Go, the system uses the gRPC framework and Protocol Buffers to maintain a strict, type-safe communication contract between the client and server. By putting the compute layer behind a Network Load Balancer, the system can easily scale up by adding more servers to handle higher demand. This setup allows us to grow horizontally and ensures we aren't limited by the capacity of any single piece of hardware. Also, we decoupled the storage layer by using Amazon S3 for persistent file data and DynamoDB for fast metadata and permission tracking. This separation is the key to our scalability, allowing the system to dynamically spin up resources to handle massive traffic spikes.

---

## Unit Testing

We wrote a suite of unit tests to make sure the system behaves correctly under a real world scenario. These tests simulate multiple students and a professor interacting with the system at the same time, including operations such as uploading, downloading, renaming, and deleting files concurrently. We also tested edge cases like reading a file while it's being updated and deleted to ensure users never see partial or corrupted data. Finally, we verified that permissions are enforced properly, so students can only access their own files while professors have full control.

---

## Load Testing

### 1. Functional & Data Integrity

**Steady Classroom**
Simulates a normal day: navigating folders, listing files, uploading/downloading homework, and deletion.
> *Success Metric:* System holds up under typical daily usage patterns.

**Integrity Check**
Uploads files of 4 sizes (1 KB, 10 KB, 100 KB, 1 MB), downloads them, and performs a byte-by-byte comparison.
> *Success Metric:* Data is returned exactly as it was sent without corruption.

**Upload Delete Cycle**
Executes the full CRUD lifecycle: Upload → Download → Verify → Delete → Confirm.
> *Success Metric:* The full lifecycle completes without hanging or orphaned files.

**Large File**
Each of 5 VUs uploads and downloads a different file size: 1 MB, 5 MB, 10 MB, 25 MB, and 50 MB.
> *Success Metric:* Successful S3 multipart uploads and streaming downloads.

---

### 2. Security & Permissions

**Permission Test**
Attempts 6 attacks: navigating into another student's folder, uploading to class root, creating folders in shared directories, deleting shared lecture files, deleting files at class root, and unauthorized uploads to shared folders.
> *Success Metric:* All 6 unauthorized actions are strictly blocked.

**mkdir Workflow**
The professor creates shared folders; students create personal subfolders; students attempt to create folders at class/shared level.
> *Success Metric:* Role-based directory creation permissions are correctly enforced.

**Rename Workflows**
Professor renames shared directories and files; students rename their own folders and files; students attempt unauthorized renames in shared spaces.
> *Success Metric:* Users can only rename items they have explicit authority over.

---

### 3. Performance & Load Stress

**Burst Download**
All 10 students simultaneously download the same lecture file.
> *Success Metric:* System handles the sudden read spike without failure.

**Rapid Fire**
Fires 50 navigation and list requests per second sustained for 1 minute.
> *Success Metric:* Latency and error rates remain within acceptable thresholds under high-frequency load.

**Spike Test**
Ramps from 2 to 10 VUs with abrupt transitions; each VU navigates, lists, and downloads.
> *Success Metric:* The system absorbs sudden traffic surges and recovers gracefully.

**Soak Test**
10 VUs run the full upload/download/delete/list workflow continuously for 10 minutes.
> *Success Metric:* No memory leaks, connection pool issues, or performance decay over time.

**Tree Stress**
Calls `TreeDirectory` from root, college, and class depth under 10 concurrent VUs.
> *Success Metric:* Recursive queries complete without timeouts or database deadlock.

---

### 4. Concurrency & Race Conditions

**Teacher Race**
5 professor VUs simultaneously write different versions of the same file.
> *Success Metric:* No data corruption; file remains in a valid, readable state.

**Session Conflict**
Multiple VUs upload to student folders while navigation is happening concurrently; rapid bouncing between personal folders and class root.
> *Success Metric:* CAS (Compare-and-Swap) contention does not cause session failures.

**Concurrent Rename Stress**
5 VUs race to rename the same files and directories simultaneously.
> *Success Metric:* System state remains consistent; metadata is not corrupted.

**Concurrent CD Race Stress**
VUs attempt to `cd` into a directory that is being renamed by another user at the same time.
> *Success Metric:* Directory is always accessible via one valid name; no "broken" states.

---

## Summary

Throughout our load tests, we found issues with K6 itself as the testing suite. How we were navigating folders for sessions may have overlapped with sessions of the same user, causing `Invalid Argument` errors when looking for a folder a session was already in. A single CAS conflict would cause `navigate` to return false. K6's gRPC streaming `data` and `end` events for `SendAndClose` responses don't reliably fire in K6's event loop, causing uploads to not return even when they were successful inside CloudWatch. After `uploadBytes` returns, K6 immediately calls `Download` on the same file — but the server's multipart upload and metadata upload might not have completed yet, causing the download to arrive before the metadata was written in DynamoDB.

We also encountered eventual consistency in our interceptors. To address this, we tried adding `ConsistentRead` to a user lookup to ensure the current directory was not stale. The largest issue was still our upload latency due to K6 streaming events not delivering a `Send&Close` response. Our testing suite had a limitation based on its framework that artificially shows our upload latency to be higher than expected.

Overall, we set out to achieve a system that could handle concurrent students committing operations in a class with strong consistency and CAS-protected concurrent writes. We used a strict permission model to ensure students can only see what they are allowed to, and cannot overwrite another student's work. Our operations are fast, and our load tests never resulted in internal failures from our system.