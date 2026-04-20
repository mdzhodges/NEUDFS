# NEU-DFS
NEUDFS is a cloud-native distributed file system designed for academic use. It models a university filesystem where professors and students interact with a shared class directory structure, enforcing role-based access control across colleges, classes, and personal folders. The system exposes all operations over gRPC and is deployed fully on AWS. It is meant to be a second hub from canvas, allowing students to easily upload and download files for their class. It was first terminal like experience with bash-like commands, and now also a GUI.

## Project Management
Looking at the file neudfs_project_management.tsv, will show how we structured our Github Project and Github Issues. the file all_commits include a full history of commits per feature and debug branch that were created for the evolution of this project. We separated features or RPC commands by their own branch. Any big changes also needing to be made in Terraform or debug side had their own branch.

## Built Over Time
As we built the schema, we made branches and assigned ourselves to working on these features, as a feature was fleshed out and tested, we would then merge into dev all together, checking for conflicts. Throughout the lifecycle of the project, we all had written up a variety of bash scripts to run our deployments hosted and locally. As well as a script for a unit test then load test pipeline. The main features of this program were created first, with unit and load testing happening last. We made fixes along the way after unit testing and load testing to ensure our architecture works.

## Why we built this
We chose this project as it had elements that were new and old for our team. We also wanted a target audience for this project, to give us a sense of possible impact for future use cases. We were all familiar with REST, but chose gRPC for its ability to stream files fast to and from the server and client. And to not have to handle REST objects. We enjoyed the end result of our project, and there is a lot of room to grow for this.

## Future Possibilities
We could add a queue for file uploads, to handle large file sizes, and ensure eventual consistency. We could in the future add more commands fleshing out the bash experience and possibly tie to a user's github. Another improvement would be using a cache for how often we fetch user data for a session, and use the cache for TTL.

# To Run the Project Locally

To run the project locally, simply run ./run.sh in the root directory.

# To Run the Project Deployed

Navigate to the terraform folder and run terraform init && terraform apply

Then run ./run.sh from the root of the project.
