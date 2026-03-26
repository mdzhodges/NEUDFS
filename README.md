# NEU-DFS
CS6650 Final Project

To run GRPC server locally

Inside test folder

  docker run -p 8000:8000 amazon/dynamodb-local
  
Then in another terminal (still in test folder)

  go run scripts.go
  
In server folder set `DYNAMO_DB_ENDPOINT` to `http://localhost:8000`
Then in server folder, 

  go run main.go

New terminal $\rarr$ in client:

  go run main.go