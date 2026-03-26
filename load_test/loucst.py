import locust

from locust import FastHttpUser

class MyUser(FastHttpUser):
    def on_start(self):
        self.client = locust.Client()
    
    def on_stop(self):
        self.client.close()
    
