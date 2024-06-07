# Start with a base image containing golang runtime
FROM golang:1.20

# Downloading gcloud package
RUN curl https://dl.google.com/dl/cloudsdk/release/google-cloud-sdk.tar.gz > /tmp/google-cloud-sdk.tar.gz

# Installing the package
RUN mkdir -p /usr/local/gcloud \
  && tar -C /usr/local/gcloud -xvf /tmp/google-cloud-sdk.tar.gz \
  && /usr/local/gcloud/google-cloud-sdk/install.sh

# Install the gcloud component that includes the App Engine extension for Go.
RUN /usr/local/gcloud/google-cloud-sdk/bin/gcloud components install app-engine-go

# Add the gcloud command-line tool to your path.
ENV PATH $PATH:/usr/local/gcloud/google-cloud-sdk/bin

# # Run dev_appserver.py . from inside the repo
# RUN dev_appserver.py .

# Set the working directory in the container
WORKDIR /go/src/app

# Copy the current directory contents into the container at /go/src/app
COPY ../ .

# Expose port 8080 to the outside world
EXPOSE 8080

# Expose port 80 for http traffic
EXPOSE 80

# Expose port 8000 to the outside world
EXPOSE 8000

# Start the process
CMD ["python3", "../../../usr/local/gcloud/google-cloud-sdk/bin/dev_appserver.py", "--host=0.0.0.0", "--admin_host=0.0.0.0", "--enable_host_checking=False", "."]

