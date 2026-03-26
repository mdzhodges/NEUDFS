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
Then in server folder,

    go run main.go

New terminal $\rarr$ in client:

    go run main.go
  
# Dont forget to set credentials

    AWS_ACCESS_KEY_ID "fake"
    AWS_SECRET_ACCESS_KEY "fake"
    AWS_SESSION_TOKEN "fake"

TO GET THE SHARED ACCOUNT

<https://signin.aws.amazon.com/switchrole?roleName=The_Boys&account=190803021874>

Then navigate to the cloud shell and enter this command

curl -s -H "Authorization: $AWS_CONTAINER_AUTHORIZATION_TOKEN" $AWS_CONTAINER_CREDENTIALS_FULL_URI | jq -r '"set -gx AWS_ACCESS_KEY_ID \(.AccessKeyId)\nset -gx AWS_SECRET_ACCESS_KEY \(.SecretAccessKey)\nset -gx AWS_SESSION_TOKEN \(.Token)"'

Then copy the commands into aws config locally
