//go:build ios && cgo

#import <Foundation/Foundation.h>

extern void gorexNetworkResult(long long token, int failure);
static NSMutableDictionary<NSNumber *, NSURLSessionDataTask *> *networkTasks;

void gorex_network_start(long long token, const char *rawURL) {
 NSString *urlString = [NSString stringWithUTF8String:rawURL];
 dispatch_async(dispatch_get_main_queue(), ^{
  if (!networkTasks) networkTasks = [NSMutableDictionary new];
  NSMutableURLRequest *request = [NSMutableURLRequest requestWithURL:[NSURL URLWithString:urlString]];
  request.HTTPMethod = @"HEAD";
  request.timeoutInterval = 10;
  NSURLSessionDataTask *task = [NSURLSession.sharedSession dataTaskWithRequest:request completionHandler:^(NSData *data, NSURLResponse *response, NSError *error) {
   dispatch_async(dispatch_get_main_queue(), ^{
    [networkTasks removeObjectForKey:@(token)];
    int failure = 0;
    if (error) {
     failure = 3;
     if ([error.domain isEqualToString:NSURLErrorDomain]) {
      if (error.code == NSURLErrorNotConnectedToInternet || error.code == NSURLErrorDataNotAllowed || error.code == NSURLErrorInternationalRoamingOff) failure = 1;
      else if (error.code == NSURLErrorTimedOut) failure = 2;
     }
    }
    gorexNetworkResult(token, failure);
   });
  }];
  networkTasks[@(token)] = task;
  [task resume];
 });
}

void gorex_network_cancel(long long token) {
 dispatch_async(dispatch_get_main_queue(), ^{
  [networkTasks[@(token)] cancel];
  [networkTasks removeObjectForKey:@(token)];
 });
}
