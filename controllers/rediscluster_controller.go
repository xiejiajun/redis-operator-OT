/*
Copyright 2020 Opstree Solutions.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"strconv"
	"time"

	"redis-operator/k8sutils"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redisv1beta1 "redis-operator/api/v1beta1"
)

// RedisClusterReconciler reconciles a RedisCluster object
type RedisClusterReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// Reconcile is part of the main kubernetes reconciliation loop
func (r *RedisClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling opstree redis Cluster controller")
	instance := &redisv1beta1.RedisCluster{}

	err := r.Client.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		// TODO CRD对象不存在，忽略本次事件
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// TODO 设置自己的owner引用为自己？
	if err := controllerutil.SetControllerReference(instance, instance, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	// TODO 创建Redis Cluster Leader节点
	err = k8sutils.CreateRedisLeader(instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	// TODO 为Redis Cluster Leader创建Service (headless service & 暴露redis leader 6379端口的ClusterIP svc)
	err = k8sutils.CreateRedisLeaderService(instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	// TODO  创建Redis Cluster Follower
	err = k8sutils.CreateRedisFollower(instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	// TODO 为Redis Cluster Follower创建Service(headless service & 暴露redis leader 6379端口的ClusterIP svc)
	err = k8sutils.CreateRedisFollowerService(instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	// TODO 获取Leader STS ， 判断是否创建成功
	redisLeaderInfo, err := k8sutils.GetStateFulSet(instance.Namespace, instance.ObjectMeta.Name+"-leader")
	if err != nil {
		return ctrl.Result{}, err
	}
	// TODO 获取Follower STS, 判断是否创建成功
	redisFollowerInfo, err := k8sutils.GetStateFulSet(instance.Namespace, instance.ObjectMeta.Name+"-follower")
	if err != nil {
		return ctrl.Result{}, err
	}

	leaderReplicas := instance.Spec.RedisLeader.Replicas
	if leaderReplicas == nil {
		leaderReplicas = instance.Spec.Size
	}
	followerReplicas := instance.Spec.RedisFollower.Replicas
	if followerReplicas == nil {
		followerReplicas = instance.Spec.Size
	}
	totalReplicas := int(*leaderReplicas) + int(*followerReplicas)

	if *leaderReplicas == 0 {
		reqLogger.Info("Redis leaders Cannot be 0", "Ready.Replicas", strconv.Itoa(int(redisLeaderInfo.Status.ReadyReplicas)), "Expected.Replicas", instance.Spec.Size)
		return ctrl.Result{RequeueAfter: time.Second * 120}, nil
	}

	if int(redisLeaderInfo.Status.ReadyReplicas) != int(*leaderReplicas) && int(redisFollowerInfo.Status.ReadyReplicas) != int(*followerReplicas) {
		reqLogger.Info("Redis leader and follower nodes are not ready yet", "Ready.Replicas", strconv.Itoa(int(redisLeaderInfo.Status.ReadyReplicas)), "Expected.Replicas", instance.Spec.Size)
		return ctrl.Result{RequeueAfter: time.Second * 120}, nil
	}
	reqLogger.Info("Creating redis cluster by executing cluster creation commands", "Leaders.Ready", strconv.Itoa(int(redisLeaderInfo.Status.ReadyReplicas)), "Followers.Ready", strconv.Itoa(int(redisFollowerInfo.Status.ReadyReplicas)))
	if k8sutils.CheckRedisNodeCount(instance, "") != int(totalReplicas) {
		leaderCount := k8sutils.CheckRedisNodeCount(instance, "leader")
		if leaderCount != int(*leaderReplicas) {
			reqLogger.Info("Not all leader are part of the cluster...", "Leaders.Count", leaderCount, "Instance.Size", leaderReplicas)
			// TODO 创建cluster: redis-cli --cluster create
			k8sutils.ExecuteRedisClusterCommand(instance)
		} else {
			if *followerReplicas > 0 {
				reqLogger.Info("All leader are part of the cluster, adding follower/replicas", "Leaders.Count", leaderCount, "Instance.Size", leaderReplicas, "Follower.Replicas", followerReplicas)
				// TODO 通过Redis cli将Follower节点依次添加到集群中redis-cli --cluster add-node  ... --cluster-slave
				//  https://www.cnblogs.com/zhoujinyi/p/11606935.html
				k8sutils.ExecuteRedisReplicationCommand(instance)
			} else {
				reqLogger.Info("no follower/replicas configured, skipping replication configuration", "Leaders.Count", leaderCount, "Leader.Size", leaderReplicas, "Follower.Replicas", followerReplicas)
			}
		}
	} else {
		reqLogger.Info("Redis leader count is desired")
		if k8sutils.CheckRedisClusterState(instance) >= int(totalReplicas)-1 {
			reqLogger.Info("Redis leader is not desired, executing failover operation")
			// TODO 故障转移操作:
			//  当被删除掉的节点重新起来之后不能自动加入集群，但其和主的复制还是正常的，也可以通过该节点看到集群信息
			//  （通过其他正常节点已经看不到该被del-node节点的信息）。
			//  如果想要再次加入集群，则需要先在该节点执行cluster reset，再用add-node进行添加，进行增量同步复制。
			//  https://www.cnblogs.com/zhoujinyi/p/11606935.html
			k8sutils.ExecuteFailoverOperation(instance)
		}
		return ctrl.Result{RequeueAfter: time.Second * 120}, nil
	}
	reqLogger.Info("Will reconcile redis cluster operator in again 10 seconds")
	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RedisClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&redisv1beta1.RedisCluster{}).
		Complete(r)
}
