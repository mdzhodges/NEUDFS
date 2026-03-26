# NEU-DFS

CS6650 Final Project

# NEU-DFS

CS6650 Final Project

To run GRPC server locally

Inside test folder

    docker run -p 8000:8000 amazon/dynamodb-local
  
Then in another terminal (still in test folder)

    go run scripts.go
  
In server folder set `DYNAMODB_ENDPOINT` to `http://localhost:8000`
export DYNAMODB_ENDPOINT="http://localhost:8000"

Then in server folder, 

    go run main.go

New terminal $\rarr$ in client:

    go run main.go
  
  
# Dont forget to set credentials!

    set -x AWS_ACCESS_KEY_ID "fake"
    set -x AWS_SECRET_ACCESS_KEY "fake"
    set -x AWS_SESSION_TOKEN "fake"

    ---

    Bash Commands

    export AWS_ACCESS_KEY_ID="fake"
    export AWS_SECRET_ACCESS_KEY="fake"
    export AWS_SESSION_TOKEN="fake"


# Running the test
In test directory:
    K6_WEB_DASHBOARD=true K6_WEB_DASHBOARD_EXPORT=dashboard_report.html k6 run neudfs_test.js
    open dashboard_report.html
    open neudfs_report.html