podTemplate(
    cloud: 'kubernetes',
    containers: [
        containerTemplate(
            alwaysPullImage: false,
            envVars: [],
            command: 'dockerd-entrypoint.sh',
            args: '--storage-driver=overlay',
            image: 'docker:dind',
            name: 'docker',
            // resourceRequestCpu: '1750m',
            // resourceRequestMemory: '1500Mi',
            privileged: true,
            ttyEnabled: true,
        ),
    ],
    volumes: [
        emptyDirVolume(
            mountPath: '/var/lib/docker',
            memory: false,
        ),
    ],
    idleMinutes: 5,
    instanceCap: 5,
    label: 'kube-chargeback-build',
    name: 'kube-chargeback-build',
) {
    node ('kube-chargeback-build') {
        def gitCommit;
        try {
            container('docker'){

                def kubeChargebackDir = "${env.WORKSPACE}/go/src/github.com/coreos-inc/kube-chargeback"
                stage('checkout') {
                    sh """
                    apk update
                    apk add git bash
                    """

                    checkout([
                        $class: 'GitSCM',
                        branches: scm.branches,
                        extensions: scm.extensions + [[$class: 'RelativeTargetDirectory', relativeTargetDir: kubeChargebackDir]],
                        userRemoteConfigs: scm.userRemoteConfigs
                    ])

                    gitCommit = sh(returnStdout: true, script: "cd ${kubeChargebackDir} && git rev-parse HEAD").trim()
                    echo "Git Commit: ${gitCommit}"
                }

                withCredentials([
                    usernamePassword(credentialsId: 'quay-coreos-jenkins-push', passwordVariable: 'DOCKER_PASSWORD', usernameVariable: 'DOCKER_USERNAME'),
                ]) {

                    echo "Authenticating to docker registry"
                    // Run separately so variables are interpolated by groovy, note the double quotes
                    sh "docker login -u $DOCKER_USERNAME -p $DOCKER_PASSWORD quay.io"
                }

                stage('install dependencies') {
                    // Build & install thrift
                    sh """#!/bin/bash
                    set -e
                    apk add make go libtool autoconf automake bison libc-dev g++ boost curl
                    """
                }

                stage('build') {
                    // Build & install thrift if it's not already installed.
                    // These pod agents may be re-used, and this will speed up
                    // subsequent builds
                    sh """#!/bin/bash
                    set -e
                    if ! command -v thrift >/dev/null 2>&1; then
                      curl \
                        http://mirrors.sonic.net/apache/thrift/0.10.0/thrift-0.10.0.tar.gz \
                        -o ./thrift-0.10.0.tar.gz \
                        -z ./thrift-0.10.0.tar.gz
                      tar -xvf thrift-0.10.0.tar.gz
                      cd thrift-0.10.0
                      ./configure --without-python
                      make install
                    fi
                    """

                    sh """#!/bin/bash
                    export GOPATH=${env.WORKSPACE}/go
                    cd ${kubeChargebackDir}
                    make docker-build
                    """
                }

                stage('push') {
                    sh """
                    make docker-push
                    """
                }
            }

        } catch (e) {
            // If there was an exception thrown, the build failed
            echo "Build failed"
            currentBuild.result = "FAILED"
            throw e
        } finally {
            // notifyBuild(currentBuild.result)
        }
    }
}
