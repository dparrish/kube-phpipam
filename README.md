# Kube-phpPIAM

This controller listens to the Kubernetes API for changes in Services. Services
with IP addresses matching a configurable set will have their IP addresses
updated in an external [phpIPAM](https://phpipam.net) instance.

## Create configuration

There is a sample config.yaml file included in this repository. You will need
to change the details to point to your phpIPAM instance and provide a real
username and password that must be created as a user in phpIPAM.

Once you've created the config file, add it as a configmap in K8s:

```shell
$ kubectl create configmap kube-phpipam-config --from-file=config.yaml
```

## Run kube-phpipam

A Kubernetes yaml file is included that will create a service account with the
appropriate permissions, and start kube-phpipam as a single-pod deployment.

```shell
$ kubectl apply -f kube-phpipam.yaml
```

## Build a container

If you want to use HTTPS to talk to phpIPAM you will need to build a custom
image containing the SSL certificates. You can do that with a simple Dockerfile
such as this:

```docker
FROM dparrish/kube-phpipam:1.0.0
COPY *.crt /usr/local/share/ca-certificates/
RUN update-ca-certificates
```

